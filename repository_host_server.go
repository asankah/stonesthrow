package stonesthrow

import (
	"fmt"
	"golang.org/x/net/context"
	"regexp"
	"strconv"
	"strings"
)

type RepositoryHostServerImpl struct {
	Host         *HostConfig
	ProcessAdder ProcessAdder
}

type RepositoryGetter interface {
	GetRepository() string
}

func (r *RepositoryHostServerImpl) GetRepository(rg RepositoryGetter) (*RepositoryConfig, error) {
	repo_name := rg.GetRepository()
	repo, ok := r.Host.Repositories[repo_name]
	if !ok {
		return nil, NewInvalidRepositoryError("%s not found", repo_name)
	}
	return repo, nil
}

func (r *RepositoryHostServerImpl) GetExecutor(s JobEventSender, repo *RepositoryConfig) Executor {
	return NewJobEventExecutor(repo.Host.Name, repo.SourcePath, r.ProcessAdder, s)
}

func (r *RepositoryHostServerImpl) GetRepositoryHostServer() RepositoryHostServer {
	return r
}

func (r *RepositoryHostServerImpl) GetScriptHostRunner(repo *RepositoryConfig) ScriptHostRunner {
	var config Config
	config.Select(r.Host, repo, repo.AnyPlatform())
	return ScriptHostRunner{Config: config, ProcessAdder: r.ProcessAdder}
}

func (r *RepositoryHostServerImpl) GetGitCommandsForJobEventSender(s JobEventSender, repo *RepositoryConfig) (Executor, RepositoryCommands) {
	executor := r.GetExecutor(s, repo)
	commands := RepositoryCommands{Repository: repo, Executor: executor}
	return executor, commands
}

func (r *RepositoryHostServerImpl) GetRepositoryUpstreamPeer(ctx context.Context, repo *RepositoryConfig) (RepositoryHostClient, error) {
	var remote_config Config
	remote_config.SelectRepository(repo.GitConfig.RemoteHost.Repositories[repo.Name])

	var local_config Config
	local_config.SelectRepository(repo)

	rpc_connection, err := ConnectTo(ctx, local_config, remote_config)
	if err != nil {
		return nil, err
	}
	remote_repo_client := NewRepositoryHostClient(rpc_connection)
	return remote_repo_client, nil
}

func SelectMatchingBranchConfigs(branches []string, branch_configs []*GitRepositoryInfo_Branch) []*GitRepositoryInfo_Branch {
	if len(branches) == 0 {
		return branch_configs
	}
	filtered_branches := []*GitRepositoryInfo_Branch{}
	allowed_branch_set := make(map[string]bool)
	for _, branch := range branches {
		allowed_branch_set[branch] = true
	}
	for _, branch := range branch_configs {
		if _, ok := allowed_branch_set[branch.GetName()]; ok {
			filtered_branches = append(filtered_branches, branch)
		}
	}
	return filtered_branches
}

func (r *RepositoryHostServerImpl) GetBranchConfig(ctx context.Context, co *BranchConfigOptions) (*GitRepositoryInfo, error) {
	repo, err := r.GetRepository(co)
	if err != nil {
		return nil, err
	}
	properties := repo.GitConfig.SyncableProperties
	_, commands := r.GetGitCommandsForJobEventSender(nil, repo)
	propertySet := make(map[string]bool)
	for _, property := range properties {
		propertySet[property] = true
	}

	branchSet := make(map[string]*GitRepositoryInfo_Branch)
	branch_filter := co.GetBranchSpec()
	if branch_filter == "" {
		branch_filter = ".*"
	}

	branch_config_filter := "^branch\\." + branch_filter + "\\.base-upstream$"
	if co.GetIncludeGitConfig() {
		branch_config_filter = "^branch\\." + branch_filter
	}

	allPropertiesString, err := commands.ExecuteNoStream(ctx, "git", "config", "--local", "-z", "--get-regex", branch_config_filter)
	if err != nil {
		return nil, err
	}

	configLines := strings.Split(allPropertiesString, "\x00")
	for _, configLine := range configLines {
		if len(configLine) == 0 {
			continue
		}
		fields := strings.Split(configLine, "\n")
		if len(fields) != 2 {
			return nil, fmt.Errorf("Unexpected config format. Config line is : %s (%d)", configLine, len(configLine))
		}

		name := fields[0]
		value := fields[1]

		name_components := strings.Split(name, ".")
		if len(name_components) != 3 {
			return nil, fmt.Errorf("Unexpected config format. Config line is : %s (%d)", configLine, len(configLine))
		}

		if name_components[0] != "branch" {
			return nil, fmt.Errorf("Unexpected name field %s in %s", name_components[0], configLine)
		}

		c, ok := branchSet[name_components[1]]
		if !ok {
			c = &GitRepositoryInfo_Branch{Name: name_components[1], Config: make(map[string]string)}
			branchSet[name_components[1]] = c
		}

		_, ok = propertySet[name_components[2]]
		if !ok {
			continue
		}

		c.Config[name_components[2]] = value
	}

	for _, c := range branchSet {
		revision, err := commands.GitRevision(ctx, c.Name)
		if err != nil {
			delete(branchSet, c.Name)
			continue
		}
		c.Revision = revision

		upstream, ok := c.Config["base-upstream"]
		if !ok {
			continue
		}
		counts_string, err := commands.Execute(ctx, "git", "rev-list", "--left-right", "--count",
			fmt.Sprintf("%s...%s", c.Name, upstream))
		if err != nil {
			continue
		}
		counts_fields := strings.Split(counts_string, "\t")
		if len(counts_fields) != 2 {
			return nil, fmt.Errorf("Unexpected format for 'git rev-list --count --left-right': [%s]", counts_string)
		}
		converted, _ := strconv.ParseInt(counts_fields[0], 10, 32)
		c.RevisionsAhead = int32(converted)
		converted, _ = strconv.ParseInt(counts_fields[1], 10, 32)
		c.RevisionsBehind = int32(converted)
	}

	ri := &GitRepositoryInfo{}

	branch_list := []*GitRepositoryInfo_Branch{}
	for _, c := range branchSet {
		branch_list = append(branch_list, c)
	}
	ri.Branches = branch_list

	remotes_map := make(map[string]*GitRepositoryInfo_Upstream)
	allRemotesString, err := commands.ExecuteNoStream(ctx, "git", "remote", "--verbose")
	if err != nil {
		return ri, err
	}
	remote_line_regex := regexp.MustCompile(`^(\w*)\t([^ ]*) \((\w*)\)$`)
	for _, remote_string := range strings.Split(allRemotesString, "\n") {
		fields := remote_line_regex.FindStringSubmatch(remote_string)
		if fields == nil {
			continue
		}
		remote, ok := remotes_map[fields[1]]
		if !ok {
			remote = &GitRepositoryInfo_Upstream{Name: fields[1]}
			remotes_map[fields[1]] = remote
		}

		if fields[3] == "fetch" {
			remote.FetchUrl = fields[2]
		} else if fields[3] == "push" {
			remote.PushUrl = fields[2]
		}
	}

	remote_list := []*GitRepositoryInfo_Upstream{}
	for _, u := range remotes_map {
		remote_list = append(remote_list, u)
	}
	ri.Upstreams = remote_list

	return ri, nil
}

func (r *RepositoryHostServerImpl) SetBranchConfig(info *GitRepositoryInfo, s RepositoryHost_SetBranchConfigServer) error {

	repo, err := r.GetRepository(info)
	if err != nil {
		return err
	}
	_, commands := r.GetGitCommandsForJobEventSender(s, repo)
	for _, branch := range info.GetBranches() {
		revision, err := commands.ExecuteNoStream(s.Context(), "git", "rev-parse", branch.GetName())
		if err != nil {
			return fmt.Errorf("Unknown branch %s", branch.GetName())
		}

		if revision != branch.GetRevision() {
			return fmt.Errorf("Revision mismatch for branch %s. actual %s vs expected %s",
				branch.GetName(), revision, branch.GetRevision())
		}

		for name, value := range branch.GetConfig() {
			configName := fmt.Sprintf("branch.%s.%s", branch.GetName(), name)
			err := commands.ExecutePassthrough(s.Context(), "git", "config", "--local", configName, value)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *RepositoryHostServerImpl) PullFromUpstream(list *BranchList, s RepositoryHost_PullFromUpstreamServer) error {
	repo, err := r.GetRepository(list)
	if err != nil {
		return err
	}
	_, commands := r.GetGitCommandsForJobEventSender(s, repo)

	if old_branch, err := commands.GitCurrentBranch(s.Context()); err == nil {
		err = commands.GitCheckoutRevision(s.Context(), "origin/master")
		if err == nil {
			defer commands.GitCheckoutRevision(s.Context(), old_branch)
		}
	}

	if repo.GitConfig.RemoteHost == nil {
		return nil
	}

	remote_repo_client, err := r.GetRepositoryUpstreamPeer(s.Context(), repo)
	if err != nil {
		return err
	}

	options := BranchConfigOptions{IncludeGitConfig: true}

	remote_repo_info, err := remote_repo_client.GetBranchConfig(s.Context(), &options)
	if err != nil {
		return err
	}
	remote_repo_info.Branches = SelectMatchingBranchConfigs(list.GetBranch(), remote_repo_info.GetBranches())

	local_repo_info, err := r.GetBranchConfig(s.Context(), &options)
	if err != nil {
		return err
	}
	local_repo_info.Branches = SelectMatchingBranchConfigs(list.GetBranch(), local_repo_info.GetBranches())

	err = commands.GitFetch(s.Context(), list.GetBranch())
	if err != nil {
		return err
	}

	return r.SetBranchConfig(remote_repo_info, s)
}

func (r *RepositoryHostServerImpl) PushToUpstream(list *BranchList, s RepositoryHost_PushToUpstreamServer) error {
	repo, err := r.GetRepository(list)
	if err != nil {
		return err
	}
	e, commands := r.GetGitCommandsForJobEventSender(s, repo)
	branches := list.GetBranch()

	if len(branches) == 0 {
		return NewInvalidArgumentError("No branches")
	}

	if len(branches) == 1 && branches[0] == "HEAD" {
		var err error
		branches[0], err = commands.GitCurrentBranch(s.Context())
		if err != nil {
			return err
		}
	}

	repo_state, err := GetRepositoryState(s.Context(), repo, e, false)
	if err != nil {
		return err
	}

	remote_repo_client, err := r.GetRepositoryUpstreamPeer(s.Context(), repo)
	if err != nil {
		return err
	}

	jobevent_receiver, err := remote_repo_client.PrepareForReceive(s.Context(), repo_state)
	if err != nil {
		return err
	}
	DrainJobEventPipe(jobevent_receiver, s)

	err = commands.GitPush(s.Context(), branches, false)
	if err != nil {
		return err
	}

	options := BranchConfigOptions{IncludeGitConfig: true}
	repo_info, err := r.GetBranchConfig(s.Context(), &options)
	if err != nil {
		return err
	}

	repo_info.Branches = SelectMatchingBranchConfigs(list.GetBranch(), repo_info.GetBranches())
	jobevent_receiver, err = remote_repo_client.SetBranchConfig(s.Context(), repo_info)
	if err != nil {
		return err
	}
	DrainJobEventPipe(jobevent_receiver, s)
	return nil
}

func (r *RepositoryHostServerImpl) Status(rs *RepositoryState, s RepositoryHost_StatusServer) error {
	repo, err := r.GetRepository(rs)
	if err != nil {
		return err
	}
	_, commands := r.GetGitCommandsForJobEventSender(s, repo)
	return commands.ExecutePassthrough(s.Context(), "git", "status")
}

func (r *RepositoryHostServerImpl) SyncRemote(rs *RepositoryState, s RepositoryHost_SyncRemoteServer) error {
	repo, err := r.GetRepository(rs)
	if err != nil {
		return err
	}
	_, commands := r.GetGitCommandsForJobEventSender(s, repo)

	// GitCheckoutRevision automatically fetches the BUILDER_HEAD revision
	// if it can't resolve rs.GetRevision().
	err = commands.GitCheckoutRevision(s.Context(), rs.GetRevision())
	if err != nil {
		return err
	}

	return r.GetScriptHostRunner(repo).OnRepositoryCheckout(s.Context(), r.GetExecutor(s, repo), s)
}

func (r *RepositoryHostServerImpl) PrepareForReceive(rs *RepositoryState, s RepositoryHost_PrepareForReceiveServer) error {
	repo, err := r.GetRepository(rs)
	if err != nil {
		return err
	}
	_, commands := r.GetGitCommandsForJobEventSender(s, repo)
	return commands.GitCheckoutRevision(s.Context(), "origin/master")
}

func (r *RepositoryHostServerImpl) FetchFile(fo *FetchFileOptions, s RepositoryHost_FetchFileServer) error {
	repo, err := r.GetRepository(fo)
	if err != nil {
		return err
	}
	return SendFiles(s.Context(), repo.SourcePath, fo, s)
}

func (r *RepositoryHostServerImpl) RunScriptCommand(ro *RunOptions, s RepositoryHost_RunScriptCommandServer) error {
	repo, err := r.GetRepository(ro)
	if err != nil {
		return err
	}
	return r.GetScriptHostRunner(repo).RunScriptCommand(ro, r.GetExecutor(s, repo), s)
}

func (r *RepositoryHostServerImpl) ListScriptCommands(ctx context.Context, l *ListCommandsOptions) (*CommandList, error) {
	repo, err := r.GetRepository(l)
	if err != nil {
		return nil, err
	}

	return r.GetScriptHostRunner(repo).ListScriptCommands(ctx, r.GetExecutor(nil, repo))
}

func (r *RepositoryHostServerImpl) RunShellCommand(ro *RunOptions, s RepositoryHost_RunShellCommandServer) error {
	repo, err := r.GetRepository(ro)
	if err != nil {
		return err
	}
	e := r.GetExecutor(s, repo)
	script_host_runner := r.GetScriptHostRunner(repo)
	return e.ExecuteInWorkDirPassthrough(
		script_host_runner.ExpandTokens(ro.GetCommand().GetDirectory()),
		s.Context(),
		script_host_runner.ExpandTokensInArray(ro.GetCommand().GetCommand())...)
}

func GetRepositoryState(ctx context.Context, r *RepositoryConfig, e Executor, push_builder_head bool) (*RepositoryState, error) {
	var revision string
	var err error
	commands := RepositoryCommands{Repository: r, Executor: e}
	revision, err = commands.GitCreateBuilderHead(ctx)
	if err != nil {
		return nil, err
	}
	if push_builder_head {
		err = commands.GitPushBuilderHead(ctx)

		// If there is no upstream, then there's no need to push the BUILDER_HEAD anyway.
		if IsNoUpstreamError(err) {
			err = nil
		}
	}
	return &RepositoryState{
		Repository: r.Name,
		Revision:   revision}, err
}

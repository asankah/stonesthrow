package stonesthrow

import (
	"fmt"
	"golang.org/x/net/context"
	"regexp"
	"strconv"
	"strings"
)

type RepositoryHostServerImpl struct {
	Repository   *RepositoryConfig
	ProcessAdder ProcessAdder
}

func (r *RepositoryHostServerImpl) GetGitCommandsForJobEventSender(s JobEventSender) (Executor, RepositoryCommands) {
	executor := NewJobEventExecutor(r.Repository.Host.Name, r.Repository.SourcePath, r.ProcessAdder, s)
	commands := RepositoryCommands{Repository: r.Repository, Executor: executor}
	return executor, commands
}

func (r *RepositoryHostServerImpl) GetBranchConfig(ctx context.Context, rs *RepositoryState) (*GitRepositoryInfo, error) {
	properties := r.Repository.GitConfig.SyncableProperties
	_, commands := r.GetGitCommandsForJobEventSender(nil)
	propertySet := make(map[string]bool)
	for _, property := range properties {
		propertySet[property] = true
	}

	branchSet := make(map[string]*GitRepositoryInfo_Branch)

	allPropertiesString, err := commands.ExecuteNoStream(ctx, "git", "config", "--local", "-z", "--get-regex", "^branch\\..*")
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
			fmt.Printf("No upstream for %s\n", c.Name)
			continue
		}
		counts_string, err := commands.Execute(ctx, "git", "rev-list", "--left-right", "--count",
			fmt.Sprintf("%s...%s", c.Name, upstream))
		if err != nil {
			fmt.Printf("Failed to execute git-rev-list [%s]\n", counts_string)
			continue
		}
		fmt.Printf("%s:%s -> %s\n", c.Name, upstream, counts_string)
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

	_, commands := r.GetGitCommandsForJobEventSender(s)
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
	_, commands := r.GetGitCommandsForJobEventSender(s)

	if len(list.GetBranch()) == 0 {
		return NewInvalidArgumentError("No branches")
	}

	return commands.GitFetch(s.Context(), list.GetBranch())
}

func (r *RepositoryHostServerImpl) PushToUpstream(list *BranchList, s RepositoryHost_PushToUpstreamServer) error {
	_, commands := r.GetGitCommandsForJobEventSender(s)

	if len(list.GetBranch()) == 0 {
		return NewInvalidArgumentError("No branches")
	}

	return commands.GitPush(s.Context(), list.GetBranch())
}

func (r *RepositoryHostServerImpl) Status(rs *RepositoryState, s RepositoryHost_StatusServer) error {
	_, commands := r.GetGitCommandsForJobEventSender(s)
	return commands.ExecutePassthrough(s.Context(), "git", "status")
}

func (r *RepositoryHostServerImpl) SyncLocal(rs *RepositoryState, s RepositoryHost_SyncLocalServer) error {
	_, commands := r.GetGitCommandsForJobEventSender(s)
	return commands.ExecutePassthrough(s.Context(), "gclient", "sync")
}

func (r *RepositoryHostServerImpl) SyncRemote(rs *RepositoryState, s RepositoryHost_SyncRemoteServer) error {
	_, commands := r.GetGitCommandsForJobEventSender(s)

	old_deps_hash, _ := commands.GitHashObject(s.Context(), r.Repository.RelativePath("DEPS"))
	_, err := commands.GitTreeForRevision(s.Context(), rs.GetRevision())
	if err != nil {
		err = commands.GitFetchBuilderHead(s.Context())
	}
	if err != nil {
		return err
	}
	err = commands.GitCheckoutRevision(s.Context(), rs.GetRevision())
	if err != nil {
		return err
	}
	new_deps_hash, _ := commands.GitHashObject(s.Context(), r.Repository.RelativePath("DEPS"))
	if old_deps_hash != new_deps_hash {
		s.Send(&JobEvent{LogEvent: &LogEvent{
			Host:     r.Repository.Host.Name,
			Msg:      "DEPS changed. Running 'sync'",
			Severity: LogEvent_INFO}})
		return commands.ExecutePassthrough(s.Context(), "gclient", "sync")
	}
	return nil
}

func (r *RepositoryHostServerImpl) PrepareForReceive(rs *RepositoryState, s RepositoryHost_PrepareForReceiveServer) error {
	_, commands := r.GetGitCommandsForJobEventSender(s)
	return commands.GitCheckoutRevision(s.Context(), "origin/master")
}

func (r *RepositoryHostServerImpl) RebaseUpdate(rs *RepositoryState, s RepositoryHost_RebaseUpdateServer) error {
	_, commands := r.GetGitCommandsForJobEventSender(s)

	if r.Repository.GitConfig.Remote == "" {
		return NewNoUpstreamError("repository: %s", r.Repository.Name)
	}

	status, err := commands.GitStatus(s.Context())
	if err != nil {
		return err
	}
	if status.HasUnmerged {
		return NewUnmergedChangesExistError("can't rebase-update with dirty tree")
	}

	previousHead, _ := commands.GitCurrentBranch(s.Context())

	err = commands.ExecuteInWorkDirPassthrough(r.Repository.RelativePath("clank"), s.Context(), "git", "checkout", "origin/master")
	if err != nil {
		return err
	}

	err = commands.ExecuteInWorkDirPassthrough(r.Repository.RelativePath("clank"), s.Context(), "git", "pull", "origin", "master")
	if err != nil {
		return err
	}

	err = commands.ExecuteInWorkDirPassthrough(r.Repository.SourcePath, s.Context(), "git", "checkout", "origin/master")
	if err != nil {
		return err
	}

	err = commands.ExecuteInWorkDirPassthrough(r.Repository.SourcePath, s.Context(), "git", "pull", "origin", "master")
	if err != nil {
		return err
	}

	err = r.SyncLocal(rs, s)
	if err != nil {
		return err
	}

	err = commands.ExecutePassthrough(s.Context(), "git", "clean", "-f")
	err = commands.ExecutePassthrough(s.Context(), "git", "rebase-update", "--no-fetch", "--keep-going")
	if previousHead != "" {
		commands.ExecutePassthrough(s.Context(), "git", "checkout", previousHead)
	}

	return err
}

func GetRepositoryState(ctx context.Context, r *RepositoryConfig, e Executor, create_builder_head bool) (*RepositoryState, error) {
	var revision string
	var err error
	commands := RepositoryCommands{Repository: r, Executor: e}
	if create_builder_head {
		revision, err = commands.GitCreateBuilderHead(ctx)
	} else {
		revision, err = commands.GitRevision(ctx, "HEAD")
	}
	if err != nil {
		return nil, err
	}
	return &RepositoryState{
		Repository: r.Name,
		Revision:   revision}, nil
}

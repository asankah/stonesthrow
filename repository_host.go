package stonesthrow

import (
	"context"
)

type RepositoryHostServerImpl struct {
	Repository   *RepositoryConfig
	ProcessAdder ProcessAdder
}

func (r *RepositoryHostServerImpl) GetGitCommandsForServer(s JobEventSender) (Executor, RepositoryCommands) {
	executor := NewJobEventExecutor(r.Repository.Host.Name, r.ProcessAdder, s)
	commands := RepositoryCommands{Repository: r.Repository, Executor: executor}
	return executor, commands
}

func (r *RepositoryHostServerImpl) GetBranchConfig(ctx context.Context, rs *RepositoryState) (*GitRepositoryInfo, error) {
	_, commands := r.GetGitCommandsForServer(nil)
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
		fields := strings.Split(configLine, "\n")
		if len(fields) != 2 {
			continue
		}

		name := fields[0]
		value := fields[1]

		name_components := strings.Split(name, ".")
		if len(name_components) != 3 {
			continue
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
		counts_string, err := commands.ExecuteNoStream(ctx, "git", "rev-list", "--left-right", "--count",
			fmt.Sprintf("{}..{}", c.Name, upstream))
		if err != nil {
			continue
		}
		counts_fields := strings.Split(counts_string, "\t")
		if len(counts_fields) != 2 {
			continue
		}
		c.RevisionsBehind, _ = strconv.ParseInt(counts_fields[0], 10, 32)
		c.RevisionsAhead, _ = strconv.ParseInt(counts_fields[1], 10, 32)
	}

	ri := &GitRepositoryInfo{}

	branch_list := []GitRepositoryInfo_Branch{}
	for _, c := range branchSet {
		branch_list = append(branch_list, *c)
	}
	ri.Branches = &branch_list

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

	remote_list := []GitRepositoryInfo_Upstream{}
	for _, u := range remotes_map {
		remote_list = append(remote_list, *u)
	}
	ri.Upstreams = remote_list

	return ri, nil
}

func (r *RepositoryHostServerImpl) SetBranchConfig(info *GitRepositoryInfo, s RepositoryHost_SetBranchConfigServer) error {
	_, commands := r.GetGitCommandsForServer(s)
	for _, branch := range c.GetBranches() {
		revision, err := commands.ExecuteNoStream(ctx, "git", "rev-parse", branch.GetName())
		if err != nil {
			return fmt.Errorf("Unknown branch %s", branch.GetName())
		}

		if revision != branch.GetRevision() {
			return fmt.Errorf("Revision mismatch for branch %s. actual %s vs expected %s",
				branch.GetName(), revision, branch.GetRevision())
		}

		for name, value := range branch.GetConfig() {
			configName := fmt.Sprintf("branch.%s.%s", branch.GetName(), name)
			err := r.ExecutePassthrough(ctx, "git", "config", "--local", configName, value)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *RepositoryHostServerImpl) PullFromUpstream(list *BranchList, s RepositoryHost_PullFromUpstreamServer) error {
	_, commands := r.GetGitCommandsForServer(s)

	if len(list.GetBranch()) == 0 {
		return NewInvalidArgumentError("No branches")
	}

	err := commands.GitFetch(s.Context(), BranchListFromGitRepositoryInfo_Branch(branches))
	if err != nil {
		return err
	}
}

func GetRepositoryState(ctx context.Context, r *RepositoryConfig, e Executor) (*RepositoryState, error) {
	commands := RepositoryCommands{Repository: r, Executor: e}
	revision, err := commands.GitRevision(ctx, "HEAD")
	if err != nil {
		return nil, err
	}
	return &RepositoryState{
		Repository: r.Name,
		Revision:   revision}, nil
}

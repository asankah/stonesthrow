package stonesthrow

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type RepositoryGitConfig struct {
	SyncableProperties []string    `json:"syncable_properties,omitempty"`
	Remote             string      `json:"remote,omitempty"`
	RemoteHostname     string      `json:"hostname,omitempty"`
	RemoteHost         *HostConfig `json:"-"`
}

type RepositoryConfig struct {
	SourcePath string                     `json:"src"`
	Platforms  map[string]*PlatformConfig `json:"platforms"`
	GitConfig  RepositoryGitConfig        `json:"git"`

	Name string      `json:"-"`
	Host *HostConfig `json:"-"`
}

func (r *RepositoryConfig) Normalize(name string, hostConfig *HostConfig) error {
	r.Host = hostConfig
	r.Name = name

	for platform, platformConfig := range r.Platforms {
		err := platformConfig.Normalize(platform, r)
		if err != nil {
			return err
		}
	}
	return r.Validate()
}

func (r *RepositoryConfig) Validate() error {
	if r.Host == nil || r.Name == "" {
		return fmt.Errorf("RepositoryConfig not normalized")
	}

	if r.SourcePath == "" {
		return fmt.Errorf("SourcePath invalid for %s in %s", r.Name, r.Host.Name)
	}

	for _, p := range r.Platforms {
		err := p.Validate()
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RepositoryConfig) AnyPlatform() *PlatformConfig {
	for _, platform := range r.Platforms {
		return platform
	}
	return nil
}

func (r *RepositoryConfig) RelativePath(path string) string {
	return filepath.Join(r.SourcePath, path)
}

func (r *RepositoryConfig) RunHere(ctx context.Context, e Executor, command ...string) (string, error) {
	return e.RunCommand(ctx, r.SourcePath, command...)
}

func (r *RepositoryConfig) CheckHere(ctx context.Context, e Executor, command ...string) error {
	return e.CheckCommand(ctx, r.SourcePath, command...)
}

func (r *RepositoryConfig) GitCurrentBranch(ctx context.Context, e Executor) (string, error) {
	return e.RunCommand(ctx, r.SourcePath, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
}

func (r *RepositoryConfig) GitRevision(ctx context.Context, e Executor, name string) (string, error) {
	return r.RunHere(ctx, e, "git", "rev-parse", name)
}

func (r *RepositoryConfig) GitTreeForRevision(ctx context.Context, e Executor, name string) (string, error) {
	if runtime.GOOS == "windows" {
		return r.RunHere(ctx, e, "git", "rev-parse", fmt.Sprintf("\"%s^^^^{tree}\"", name))
	} else {
		return r.RunHere(ctx, e, "git", "rev-parse", fmt.Sprintf("%s^{tree}", name))
	}
}

// GitCreateWorkTree takes a snapshot of the working set of files, and returns a Git tree ID.
func (r *RepositoryConfig) GitCreateWorkTree(ctx context.Context, e Executor) (string, error) {
	status, err := r.GitStatus(ctx, e)
	if err != nil {
		return "", err
	}

	if status.HasUnmerged {
		return "", UnmergedChangesExistError
	}

	if !status.HasModified {
		return r.GitTreeForRevision(ctx, e, "HEAD")
	}

	_, err = r.RunHere(ctx, e, "git", "add", "-u")
	if err != nil {
		return "", err
	}
	return r.RunHere(ctx, e, "git", "write-tree")
}

func (r *RepositoryConfig) GitCreateBuilderHead(ctx context.Context, e Executor) (string, error) {
	status, err := r.GitStatus(ctx, e)
	if err != nil {
		return "", err
	}

	var tree string
	if len(status.ModifiedFiles) > 0 {
		command := []string{"git", "update-index", "--"}
		command = append(command, status.ModifiedFiles...)
		_, err = r.RunHere(ctx, e, command...)
		if err != nil {
			return "", err
		}

		tree, err = r.RunHere(ctx, e, "git", "write-tree")
		if err != nil {
			return "", err
		}
	} else {
		tree, err = r.GitTreeForRevision(ctx, e, "HEAD")
		if err != nil {
			return "", err
		}
	}

	builderTree, err := r.GitTreeForRevision(ctx, e, "BUILDER_HEAD")
	if err != nil || builderTree != tree {
		headCommit, err := r.GitRevision(ctx, e, "HEAD")
		if err != nil {
			return "", err
		}
		revision, err := r.RunHere(ctx, e, "git", "commit-tree", "-p", headCommit, "-m", "BUILDER_HEAD", tree)
		if err != nil {
			return "", err
		}
		_, err = r.RunHere(ctx, e, "git", "update-ref", "refs/heads/BUILDER_HEAD", revision)
		if err != nil {
			return "", err
		}
		return revision, nil
	}
	return r.GitRevision(ctx, e, "BUILDER_HEAD")
}

func (r *RepositoryConfig) GitPushBuilderHead(ctx context.Context, e Executor) error {
	_, err := r.GitPush(ctx, e, []string{"BUILDER_HEAD"}, false)
	return err
}

func (r *RepositoryConfig) GitFetchBuilderHead(ctx context.Context, e Executor) error {
	return r.GitFetch(ctx, e, []string{"BUILDER_HEAD"})
}

func (r *RepositoryConfig) branchListToRefspec(ctx context.Context, e Executor, branches []string) []string {
	refspecs := []string{}

	for _, branch := range branches {
		if branch == "HEAD" {
			branch, _ = r.GitCurrentBranch(ctx, e)
		}
		if branch == "" {
			continue
		}
		refspecs = append(refspecs, fmt.Sprintf("+%s:%s", branch, branch))
	}
	return refspecs
}

func (r *RepositoryConfig) GitPush(ctx context.Context, e Executor, branches []string, setUpstream bool) ([]string, error) {
	if len(branches) == 0 {
		return nil, InvalidArgumentError
	}

	if r.GitConfig.Remote == "" {
		return nil, NoUpstreamError
	}

	command := []string{"git", "push", r.GitConfig.Remote, "--porcelain", "--thin", "--force"}
	if setUpstream {
		command = append(command, "--set-upstream")
	}
	command = append(command, r.branchListToRefspec(ctx, e, branches)...)

	output, err := r.RunHere(ctx, e, command...)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "!\t") {
			return lines, FailedToPushGitBranchError
		}

		if line == "Done" {
			return lines, nil
		}
	}
	return lines, err
}

func (r *RepositoryConfig) GitFetch(ctx context.Context, e Executor, branches []string) error {
	if len(branches) == 0 {
		return InvalidArgumentError
	}
	if r.GitConfig.Remote == "" {
		return NoUpstreamError
	}
	command := []string{"git", "fetch", r.GitConfig.Remote}
	command = append(command, r.branchListToRefspec(ctx, e, append(branches, "refs/remotes/origin/master"))...)

	return r.CheckHere(ctx, e, command...)
}

func (r *RepositoryConfig) GitHashObject(ctx context.Context, e Executor, path string) (string, error) {
	return r.RunHere(ctx, e, "git", "hash-object", path)
}

func (r *RepositoryConfig) GitCheckoutRevision(ctx context.Context, e Executor, targetRevision string) error {
	currentWorkTree, err := r.GitCreateWorkTree(ctx, e)
	if err != nil {
		return err
	}
	targetWorkTree, err := r.GitTreeForRevision(ctx, e, targetRevision)
	if err == nil && currentWorkTree == targetWorkTree {
		return nil
	}

	if err != nil {
		err = r.GitFetchBuilderHead(ctx, e)
	}

	if err != nil {
		return err
	}

	err = r.CheckHere(ctx, e, "git", "checkout", "--force", "--quiet", "--no-progress", targetRevision)
	if err != nil {
		return err
	}

	return nil
}

type GitStatusResult struct {
	HasUnmerged   bool
	HasModified   bool
	ModifiedFiles []string
}

func (r *RepositoryConfig) GitStatus(ctx context.Context, e Executor) (GitStatusResult, error) {
	var result GitStatusResult
	gitStatus, err := r.RunHere(ctx, e, "git", "status", "--porcelain=2",
		"--untracked-files=no", "--ignore-submodules")
	if err != nil {
		return result, err
	}

	result.ModifiedFiles = []string{}
	scanner := bufio.NewScanner(strings.NewReader(gitStatus))
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "u ") {
			result.HasUnmerged = true
		}
		// Normal changed entry.
		if strings.HasPrefix(text, "1 ") {
			fields := strings.Split(text, " ")
			if len(fields) < 9 || len(fields[1]) != 2 || fields[1][1] == '.' {
				continue
			}
			result.ModifiedFiles = append(result.ModifiedFiles, fields[8])
			result.HasModified = true
		}

		if strings.HasPrefix(text, "2 ") {
			fields := strings.Split(text, " ")
			if len(fields) < 10 || len(fields[1]) != 2 || fields[1][1] == '.' {
				continue
			}
			paths := strings.Split(fields[9], "\t")
			if len(paths) != 2 {
				continue
			}
			result.HasModified = true
			result.ModifiedFiles = append(result.ModifiedFiles, paths[0])
		}
	}

	return result, nil
}

func (r *RepositoryConfig) GitGetBranchConfig(ctx context.Context, e Executor,
	branches []string, properties []string) ([]BranchConfig, error) {

	propertySet := make(map[string]bool)
	for _, property := range properties {
		propertySet[property] = true
	}

	branchSet := make(map[string]*BranchConfig)
	includeAllBranches := (len(branches) == 1 && branches[0] == "refs/heads/*")
	if !includeAllBranches {
		for _, branch := range branches {
			branchSet[branch] = &BranchConfig{Name: branch, GitConfig: make(map[string]string)}
		}
	}

	allPropertiesString, err := r.RunHere(ctx, e, "git", "config", "--local", "-z", "--get-regex", "^branch\\..*")
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

		namefields := strings.Split(name, ".")
		if len(namefields) != 3 {
			continue
		}

		if namefields[0] != "branch" {
			return nil, fmt.Errorf("Unexpected name field %s in %s", namefields[0], configLine)
		}

		c, ok := branchSet[namefields[1]]
		if !ok {
			if includeAllBranches {
				c = &BranchConfig{Name: namefields[1], GitConfig: make(map[string]string)}
				branchSet[namefields[1]] = c
			} else {
				continue
			}
		}

		_, ok = propertySet[namefields[2]]
		if !ok {
			continue
		}

		c.GitConfig[namefields[2]] = value
	}

	for _, c := range branchSet {
		revision, err := r.RunHere(ctx, e, "git", "rev-parse", c.Name)
		if err != nil {
			delete(branchSet, c.Name)
			continue
		}
		c.Revision = revision
	}

	configs := []BranchConfig{}
	for _, c := range branchSet {
		configs = append(configs, *c)
	}

	return configs, nil
}

func (r *RepositoryConfig) GitSetBranchConfig(ctx context.Context, e Executor,
	branchConfigs []BranchConfig) error {

	for _, config := range branchConfigs {
		revision, err := r.RunHere(ctx, e, "git", "rev-parse", config.Name)
		if err != nil {
			return fmt.Errorf("Unknown branch %s", config.Name)
		}

		if revision != config.Revision {
			return fmt.Errorf("Revision mismatch for branch %s. actual %s vs expected %s",
				config.Name, revision, config.Revision)
		}

		for name, value := range config.GitConfig {
			configName := fmt.Sprintf("branch.%s.%s", config.Name, name)
			err := r.CheckHere(ctx, e, "git", "config", "--local", configName, value)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

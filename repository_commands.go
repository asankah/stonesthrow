package stonesthrow

import (
	"bufio"
	"context"
	"fmt"
	"runtime"
	"strings"
)

type RepositoryCommands struct {
	Repository *RepositoryConfig
	Executor   Executor
}

func (r RepositoryCommands) ExecutePassthrough(c context.Context, command ...string) error {
	return r.Executor.ExecuteInWorkDirPassthrough(r.Repository.SourcePath, c, command...)
}

func (r RepositoryCommands) ExecuteNoStream(c context.Context, command ...string) (string, error) {
	return r.Executor.ExecuteInWorkDirNoStream(r.Repository.SourcePath, c, command...)
}

func (r RepositoryCommands) Execute(c context.Context, command ...string) (string, error) {
	return r.Executor.ExecuteInWorkDir(r.Repository.SourcePath, c, command...)
}

func (r RepositoryCommands) ExecuteInWorkDirPassthrough(workdir string, ctx context.Context, command ...string) error {
	return r.Executor.ExecuteInWorkDirPassthrough(workdir, ctx, command...)
}

func (r RepositoryCommands) ExecuteInWorkDir(workdir string, ctx context.Context, command ...string) (string, error) {
	return r.Executor.ExecuteInWorkDir(workdir, ctx, command...)
}

func (r RepositoryCommands) ExecuteInWorkDirNoStream(workdir string, ctx context.Context, command ...string) (string, error) {
	return r.Executor.ExecuteInWorkDirNoStream(workdir, ctx, command...)
}

func (r RepositoryCommands) GitCurrentBranch(ctx context.Context) (string, error) {
	return r.ExecuteNoStream(ctx, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
}

func (r RepositoryCommands) GitRevision(ctx context.Context, name string) (string, error) {
	return r.ExecuteNoStream(ctx, "git", "rev-parse", name)
}

func (r RepositoryCommands) GitTreeForRevision(ctx context.Context, name string) (string, error) {
	if runtime.GOOS == "windows" {
		return r.ExecuteNoStream(ctx, "git", "rev-parse", fmt.Sprintf("%s^^^^{tree}", name))
	} else {
		return r.ExecuteNoStream(ctx, "git", "rev-parse", fmt.Sprintf("%s^{tree}", name))
	}
}

// GitCreateWorkTree takes a snapshot of the working set of files, and returns a Git tree ID.
func (r RepositoryCommands) GitCreateWorkTree(ctx context.Context) (string, error) {
	status, err := r.GitStatus(ctx)
	if err != nil {
		return "", err
	}

	if status.HasUnmerged {
		return "", NewUnmergedChangesExistError("")
	}

	if !status.HasModified {
		return r.GitTreeForRevision(ctx, "HEAD")
	}

	_, err = r.ExecuteNoStream(ctx, "git", "add", "-u")
	if err != nil {
		return "", err
	}
	return r.ExecuteNoStream(ctx, "git", "write-tree")
}

func (r RepositoryCommands) GitCreateBuilderHead(ctx context.Context) (string, error) {
	status, err := r.GitStatus(ctx)
	if err != nil {
		return "", err
	}

	var tree string
	if len(status.ModifiedFiles) > 0 {
		command := []string{"git", "update-index", "--"}
		command = append(command, status.ModifiedFiles...)
		_, err = r.Execute(ctx, command...)
		if err != nil {
			return "", err
		}

		tree, err = r.Execute(ctx, "git", "write-tree")
		if err != nil {
			return "", err
		}
	} else {
		tree, err = r.GitTreeForRevision(ctx, "HEAD")
		if err != nil {
			return "", err
		}
	}

	builderTree, err := r.GitTreeForRevision(ctx, "BUILDER_HEAD")
	if err != nil || builderTree != tree {
		headCommit, err := r.GitRevision(ctx, "HEAD")
		if err != nil {
			return "", err
		}
		revision, err := r.Execute(ctx, "git", "commit-tree", "-p", headCommit, "-m", "BUILDER_HEAD", tree)
		if err != nil {
			return "", err
		}
		_, err = r.Execute(ctx, "git", "update-ref", "refs/heads/BUILDER_HEAD", revision)
		if err != nil {
			return "", err
		}
		return revision, nil
	}
	return r.GitRevision(ctx, "BUILDER_HEAD")
}

func (r RepositoryCommands) GitPushBuilderHead(ctx context.Context) error {
	err := r.GitPush(ctx, []string{"BUILDER_HEAD"})
	return err
}

func (r RepositoryCommands) GitFetchBuilderHead(ctx context.Context) error {
	return r.GitFetch(ctx, []string{"BUILDER_HEAD"})
}

func (r RepositoryCommands) branchListToRefspec(ctx context.Context, branches []string) []string {
	refspecs := []string{}

	for _, branch := range branches {
		if branch == "HEAD" {
			branch, _ = r.GitCurrentBranch(ctx)
		}
		if branch == "" {
			continue
		}
		refspecs = append(refspecs, fmt.Sprintf("+%s:%s", branch, branch))
	}
	return refspecs
}

func (r RepositoryCommands) GitPush(ctx context.Context, branches []string) error {
	if len(branches) == 0 {
		return NewInvalidArgumentError("No branches specified for 'git push'")
	}

	if r.Repository.GitConfig.Remote == "" {
		return NewNoUpstreamError("No upstream configured for repository at %s", r.Repository.SourcePath)
	}

	command := []string{"git", "push", r.Repository.GitConfig.Remote, "--porcelain", "--thin", "--force"}
	command = append(command, r.branchListToRefspec(ctx, branches)...)

	output, err := r.Execute(ctx, command...)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "!\t") {
			return NewFailedToPushGitBranchError("Result: %s", line)
		}

		if strings.HasPrefix(line, "+\t") || strings.HasPrefix(line, "*\t") || strings.HasPrefix(line, "=\t") {
			fields := strings.Split(line, "\t")
			if len(fields) != 3 {
				continue
			}
			refnamesPair := fields[1]
			refnames := strings.Split(refnamesPair, ":")
			if len(refnames) != 2 {
				continue
			}

			refsPair := fields[2]
			refs := strings.Split(refsPair, "..")
			if len(refs) != 2 {
				continue
			}

			r.Repository.GitConfig.KnownBranches[refnames[1]] = strings.Trim(refs[1], ".")
		}

		if line == "Done" {
			return nil
		}
	}
	return err
}

func (r RepositoryCommands) GitFetch(ctx context.Context, branches []string) error {
	if len(branches) == 0 {
		return NewInvalidArgumentError("No branches specified for 'git fetch'")
	}
	if r.Repository.GitConfig.Remote == "" {
		return NewNoUpstreamError("No upstream configured for repository at %s", r.Repository.SourcePath)
	}
	command := []string{"git", "fetch", r.Repository.GitConfig.Remote}
	command = append(command, r.branchListToRefspec(ctx, append(branches, "refs/remotes/origin/master"))...)

	return r.ExecutePassthrough(ctx, command...)
}

func (r RepositoryCommands) GitHashObject(ctx context.Context, path string) (string, error) {
	return r.ExecuteNoStream(ctx, "git", "hash-object", path)
}

func (r RepositoryCommands) GitCheckoutRevision(ctx context.Context, targetRevision string) error {
	currentWorkTree, err := r.GitCreateWorkTree(ctx)
	if err != nil {
		return err
	}
	targetWorkTree, err := r.GitTreeForRevision(ctx, targetRevision)
	if err == nil && currentWorkTree == targetWorkTree {
		return nil
	}

	if err != nil {
		err = r.GitFetchBuilderHead(ctx)
	}

	if err != nil {
		return err
	}

	err = r.ExecutePassthrough(ctx, "git", "checkout", "--force", "--quiet", "--no-progress", targetRevision)
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

func (r RepositoryCommands) GitStatus(ctx context.Context) (GitStatusResult, error) {
	var result GitStatusResult
	gitStatus, err := r.ExecuteNoStream(ctx, "git", "status", "--porcelain=2",
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
			if len(fields) < 9 || len(fields[1]) != 2 {
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

func (r RepositoryCommands) GitGetBranchConfig(ctx context.Context,
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

	allPropertiesString, err := r.ExecuteNoStream(ctx, "git", "config", "--local", "-z", "--get-regex", "^branch\\..*")
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
			if includeAllBranches {
				c = &BranchConfig{Name: name_components[1], GitConfig: make(map[string]string)}
				branchSet[name_components[1]] = c
			} else {
				continue
			}
		}

		_, ok = propertySet[name_components[2]]
		if !ok {
			continue
		}

		c.GitConfig[name_components[2]] = value
	}

	for _, c := range branchSet {
		revision, err := r.ExecuteNoStream(ctx, "git", "rev-parse", c.Name)
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

func (r RepositoryCommands) GitSetBranchConfig(ctx context.Context,
	branchConfigs []BranchConfig) error {

	for _, config := range branchConfigs {
		revision, err := r.ExecuteNoStream(ctx, "git", "rev-parse", config.Name)
		if err != nil {
			return fmt.Errorf("Unknown branch %s", config.Name)
		}

		if revision != config.Revision {
			return fmt.Errorf("Revision mismatch for branch %s. actual %s vs expected %s",
				config.Name, revision, config.Revision)
		}

		for name, value := range config.GitConfig {
			configName := fmt.Sprintf("branch.%s.%s", config.Name, name)
			err := r.ExecutePassthrough(ctx, "git", "config", "--local", configName, value)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

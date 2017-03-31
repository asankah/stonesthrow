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
	if !status.HasModified {
		revision, err := r.GitRevision(ctx, "HEAD")
		if err != nil {
			return "", err
		}
		_, err = r.Execute(ctx, "git", "update-ref", "refs/heads/BUILDER_HEAD", revision)
		if err != nil {
			return "", err
		}
		return revision, nil
	}

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

func (r RepositoryCommands) branchListToRefspec(ctx context.Context, branches []string, force bool) []string {
	refspecs := []string{}

	for _, branch := range branches {
		if branch == "HEAD" {
			branch, _ = r.GitCurrentBranch(ctx)
		}
		if branch == "" {
			continue
		}
		if branch == "*" || branch == "all" {
			branch = "refs/heads/*"
		}

		if force {
			refspecs = append(refspecs, fmt.Sprintf("+%s:%s", branch, branch))
		} else {
			refspecs = append(refspecs, fmt.Sprintf("%s:%s", branch, branch))
		}
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

	command := []string{"git", "push", r.Repository.GitConfig.Remote, "--porcelain", "--thin", "--force-with-lease"}
	command = append(command, r.branchListToRefspec(ctx, branches, false)...)

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
	if r.Repository.GitConfig.Remote == "" {
		return NewNoUpstreamError("No upstream configured for repository at %s", r.Repository.SourcePath)
	}
	if branches == nil || len(branches) == 0 {
		branches = []string{"*"}
	}
	command := []string{"git", "fetch", r.Repository.GitConfig.Remote}
	refspecs := r.branchListToRefspec(ctx, append(branches, "refs/remotes/origin/master"), true)
	refspecs = append(refspecs, "+refs/heads/*:refs/remotes/"+r.Repository.GitConfig.Remote+"/*")
	command = append(command, refspecs...)

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

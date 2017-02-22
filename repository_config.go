package stonesthrow

import (
	"bufio"
	"fmt"
	"strings"
	"path/filepath"
)

type RepositoryConfig struct {
	SourcePath     string                     `json:"src"`
	GitRemote      string                     `json:"git_remote,omitempty"`
	MasterHostname string                     `json:"git_hostname"`
	Platforms      map[string]*PlatformConfig `json:"platforms"`

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

func (r *RepositoryConfig) RelativePath(path string) string {
	return filepath.Join(r.SourcePath, path)
}

func (r *RepositoryConfig) RunHere(e Executor, command ...string) (string, error) {
	return e.RunCommand(r.SourcePath, command...)
}

func (r *RepositoryConfig) CheckHere(e Executor, command ...string) error {
	return e.CheckCommand(r.SourcePath, command...)
}

func (r *RepositoryConfig) GitRevision(e Executor, name string) (string, error) {
	return r.RunHere(e, "git", "rev-parse", name)
}

func (r *RepositoryConfig) GitHasUnmergedChanges(e Executor) bool {
	gitStatus, err := r.RunHere(e, "git", "status", "--porcelain=2",
		"--untracked-files=no", "--ignore-submodules")
	if err != nil {
		return false
	}

	scanner := bufio.NewScanner(strings.NewReader(gitStatus))
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "u ") {
			return true
		}
	}

	return false
}

func (r *RepositoryConfig) GitCreateWorkTree(e Executor) (string, error) {
	if r.GitHasUnmergedChanges(e) {
		return "", UnmergedChangesExistError
	}

	_, err := r.RunHere(e, "git", "add", "-u")
	if err != nil {
		return "", err
	}
	return r.RunHere(e, "git", "write-tree")
}

func (r *RepositoryConfig) GitCreateBuilderHead(e Executor) (string, error) {
	modifiedFiles, err := r.GitGetModifiedFiles(e)
	if err != nil {
		return "", err
	}

	var tree string
	if len(modifiedFiles) > 0 {
		command := []string{"git", "update-index", "--"}
		command = append(command, modifiedFiles...)
		_, err = r.RunHere(e, command...)
		if err != nil {
			return "", err
		}

		tree, err = r.RunHere(e, "git", "write-tree")
		if err != nil {
			return "", err
		}
	} else {
		tree, err = r.GitRevision(e, "HEAD^{tree}")
		if err != nil {
			return "", err
		}
	}

	builderTree, err := r.GitRevision(e, "BUILDER_HEAD^{tree}")
	if err != nil || builderTree != tree {
		headCommit, err := r.GitRevision(e, "HEAD")
		if err != nil {
			return "", err
		}
		revision, err := r.RunHere(e, "git", "commit-tree", "-p", headCommit, "-m", "BUILDER_HEAD", tree)
		if err != nil {
			return "", err
		}
		_, err = r.RunHere(e, "git", "update-ref", "refs/heads/BUILDER_HEAD", revision)
		if err != nil {
			return "", err
		}
		return revision, nil
	}
	return r.GitRevision(e, "BUILDER_HEAD")
}

func (r *RepositoryConfig) GitPush(e Executor, branch string) error {
	if r.GitRemote == "" {
		return NoUpstreamError
	}
	output, err := r.RunHere(e, "git", "push", r.GitRemote, "--porcelain", "--thin",
		"--force", branch)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "!\t") {
			return FailedToPushGitBranchError
		}

		if line == "Done" {
			return nil
		}
	}
	return err
}

func (r *RepositoryConfig) GitPushRemote(e Executor) error {
	return r.GitPush(e, "BUILDER_HEAD")
}

func (r *RepositoryConfig) GitPushCurrentBranch(e Executor) error {
	return r.GitPush(e, "HEAD")
}

func (r *RepositoryConfig) GitPullRemote(e Executor) error {
	if r.GitRemote == "" {
		return NoUpstreamError
	}
	return r.CheckHere(e, "git", "fetch", r.GitRemote, "--progress",
		"+BUILDER_HEAD:BUILDER_HEAD",
		"refs/remotes/origin/master:refs/heads/origin")
}

func (r *RepositoryConfig) GitHashObject(e Executor, path string) (string, error) {
	return r.RunHere(e, "git", "hash-object", path)
}

func (r *RepositoryConfig) GitCheckoutRevision(e Executor, targetRevision string) error {
	currentWorkTree, err := r.GitCreateWorkTree(e)
	if err != nil {
		return err
	}
	targetWorkTree, err := r.GitRevision(e, fmt.Sprintf("%s^{tree}", targetRevision))
	if err == nil && currentWorkTree == targetWorkTree {
		return nil
	}

	if err != nil {
		err = r.GitPullRemote(e)
	}

	if err != nil {
		return err
	}

	err = r.CheckHere(e, "git", "checkout", "--force", "--quiet", "--no-progress", "--detach", targetRevision)
	if err != nil {
		return err
	}

	return nil
}

func (r *RepositoryConfig) GitGetModifiedFiles(e Executor) ([]string, error) {
	gitStatus, err := r.RunHere(e, "git", "status", "--porcelain=2",
		"--untracked-files=no", "--ignore-submodules")
	if err != nil {
		return nil, err
	}

	modifiedFiles := []string{}
	scanner := bufio.NewScanner(strings.NewReader(gitStatus))
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "u ") {
			return nil, UnmergedChangesExistError
		}
		// Normal changed entry.
		if strings.HasPrefix(text, "1 ") {
			fields := strings.Split(text, " ")
			if len(fields) < 9 || len(fields[1]) != 2 || fields[1][1] == '.' {
				continue
			}
			modifiedFiles = append(modifiedFiles, fields[8])
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

			modifiedFiles = append(modifiedFiles, paths[0])
		}
	}

	return modifiedFiles, nil
}

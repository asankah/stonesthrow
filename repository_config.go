package stonesthrow

import (
	"fmt"
	"bufio"
	"strings"
)

type RepositoryConfig struct {
	SourcePath     string                     `json:"src"`
	GitRemote      string                     `json:"git_remote,omitempty"`
	MasterHostname string                     `json:"git_hostname"`
	Platforms      map[string]*PlatformConfig `json:"platforms"`

	Name string      `json:"-"`
	Host *HostConfig `json:"-"`
}

func (r *RepositoryConfig) Normalize(name string, hostConfig *HostConfig) {
	r.Host = hostConfig
	r.Name = name

	for platform, platformConfig := range r.Platforms {
		platformConfig.Normalize(platform, r)
	}
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

func (r *RepositoryConfig) RunHere(command ...string) (string, error) {
	return RunCommandWithWorkDir(r.SourcePath, command...)
}

func (r *RepositoryConfig) GitRevision(name string) (string, error) {
	return r.RunHere("git", "rev-parse", name)
}

func (r *RepositoryConfig) GitHasUnmergedChanges() bool {
	gitStatus, err := r.RunHere("git", "status", "--porcelain=2",
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

func (r *RepositoryConfig) GitCreateWorkTree() (string, error) {
	if r.GitHasUnmergedChanges() {
		return "", UnmergedChangesExistError
	}

	_, err := r.RunHere("git", "add", "-u")
	if err != nil {
		return "", err
	}
	return r.RunHere("git", "write-tree")
}


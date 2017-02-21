package stonesthrow

import (
	"fmt"
)

type SshTarget struct {
	HostName    string `json:"host"`
	SshHostName string `json:"ssh_host"`

	Host *HostConfig `json:"-"`
}

type HostConfig struct {
	Alias           []string                     `json:"alias,omitempty"`
	Repositories    map[string]*RepositoryConfig `json:"repositories,omitempty"`
	GomaPath        string                       `json:"goma_path,omitempty"`
	StonesthrowPath string                       `json:"stonesthrow,omitempty"`
	MaxBuildJobs    int                          `json:"max_build_jobs,omitempty"`
	SshTargets      []SshTarget                  `json:"ssh_targets"`

	Name              string            `json:"-"`
	DefaultRepository *RepositoryConfig `json:"-"`
}

func (h *HostConfig) Normalize(hostname string) error {
	h.Name = hostname
	for repository, repositoryConfig := range h.Repositories {
		err := repositoryConfig.Normalize(repository, h)
		if err != nil {
			return err
		}
		h.DefaultRepository = repositoryConfig
	}
	if len(h.Repositories) != 1 {
		h.DefaultRepository = nil
	}

	return h.Validate()
}

func (h *HostConfig) Validate() error {
	if h.DefaultRepository == nil || h.Name == "" {
		return fmt.Errorf("not normalized or no repositories")
	}

	for _, r := range h.Repositories {
		err := r.Validate()
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *HostConfig) SupportsPlatform(platform string) bool {
	for _, r := range h.Repositories {
		_, ok := r.Platforms[platform]
		if ok {
			return true
		}
	}

	return false
}

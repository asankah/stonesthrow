package stonesthrow

import (
	"fmt"
	"strings"
)

type CertificateLocator struct {
	CertificateFile string `json:"cert,omitempty"`
	KeyFile         string `json:"key,omitempty"`
}

type CertificateConfig struct {
	RootCert   *CertificateLocator `json:"root,omitempty"`
	ServerCert *CertificateLocator `json:"server,omitempty"`
}

type HostConfig struct {
	Alias           []string                          `json:"alias,omitempty"`
	Repositories    map[string]*RepositoryConfig      `json:"repositories,omitempty"`
	GomaPath        string                            `json:"goma_path,omitempty"`
	StonesthrowPath string                            `json:"stonesthrow,omitempty"`
	MaxBuildJobs    int                               `json:"max_build_jobs,omitempty"`
	Remotes         map[string]*RemoteTransportConfig `json:"remotes,omitempty"`
	Certificates    *CertificateConfig                `json:"certificates,omitempty"`

	Name              string            `json:"-"`
	DefaultRepository *RepositoryConfig `json:"-"`
}

func (h *HostConfig) IsWildcard() bool {
	return h.Name == "*"
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

	for _, t := range h.Remotes {
		err := t.Validate()
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

func (h *HostConfig) IsSameHost(hostname string) bool {
	if strings.EqualFold(hostname, h.Name) {
		return true
	}

	for _, alias := range h.Alias {
		if strings.EqualFold(hostname, alias) {
			return true
		}
	}

	return false
}

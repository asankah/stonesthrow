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
	ScriptPath      string                            `json:"scripts"`

	Name              string            `json:"-"`
	DefaultRepository *RepositoryConfig `json:"-"`
	HostsConfig       *HostsConfig      `json:"-"`
}

func (h *HostConfig) IsWildcard() bool {
	return h.Name == "*"
}

func (h *HostConfig) Normalize(hosts *HostsConfig) error {
	// Already normalized?
	if h.HostsConfig != nil {
		return nil
	}
	h.HostsConfig = hosts

	for remote_host, remote := range h.Remotes {
		remote.HostName = remote_host
		remote.Host, _ = hosts.Hosts[remote_host]
	}

	for repo_name, repo_config := range h.Repositories {
		err := repo_config.Normalize(repo_name, h)
		if err != nil {
			return err
		}
		h.DefaultRepository = repo_config
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

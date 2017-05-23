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
	Nickname        []string                          `json:"nickname,omitempty"`
	Repositories    map[string]*RepositoryConfig      `json:"repositories,omitempty"`
	GomaPath        string                            `json:"goma_path,omitempty"`
	GoPath          string                            `json:"go_path,omitempty"`
	StonesthrowPath string                            `json:"stonesthrow,omitempty"`
	MaxBuildJobs    int                               `json:"max_build_jobs,omitempty"`
	Remotes         map[string]*RemoteTransportConfig `json:"remotes,omitempty"`
	Certificates    *CertificateConfig                `json:"certificates,omitempty"`
	ScriptPath      string                            `json:"scripts"`
	EndpointStrings map[string]string                 `json:"endpoints"`

	Name              string              `json:"-"`
	DefaultRepository *RepositoryConfig   `json:"-"`
	HostsConfig       *HostsConfig        `json:"-"`
	Endpoints         map[string]Endpoint `json:"-"`
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

	h.Endpoints = make(map[string]Endpoint)
	for host, ep_string := range h.EndpointStrings {
		components := strings.Split(ep_string, ",")
		if len(components) == 2 {
			h.Endpoints[host] = Endpoint{
				Network:  components[0],
				Address:  components[1],
				HostName: host,
				Host:     hosts.HostByName(host)}
			if h.Endpoints[host].Host == nil {
				return fmt.Errorf("%s: Endpoint host %s can't be resolved",
					h.Name, host)
			}
		} else {
			return fmt.Errorf("Address \"%s\" was invalid. Should be of the form <network>,<address>", ep_string)
		}
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

	for _, alias := range h.Nickname {
		if strings.EqualFold(hostname, alias) {
			return true
		}
	}

	return false
}

func (p *HostConfig) GetEndpointOnHost(host *HostConfig) *Endpoint {
	ep, ok := p.Endpoints[host.Name]
	if ok && ep.Host == host {
		return &ep
	}

	for _, ep = range p.Endpoints {
		if ep.Host == host {
			return &ep
		}
	}
	return nil
}

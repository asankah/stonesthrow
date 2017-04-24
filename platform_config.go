package stonesthrow

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Endpoint struct {
	Network string
	Address string

	HostName string
	Host     *HostConfig
}

type PlatformConfig struct {
	EndpointStrings   map[string]string `json:"endpoints"`
	RelativeBuildPath string            `json:"out,omitempty"`
	MbConfigName      string            `json:"mb_config,omitempty"`

	Name       string              `json:"-"`
	BuildPath  string              `json:"-"`
	Repository *RepositoryConfig   `json:"-"`
	Endpoints  map[string]Endpoint `json:"-"`
}

func (p *PlatformConfig) Normalize(name string, repo *RepositoryConfig) error {
	hosts := repo.Host.HostsConfig
	p.Name = name
	p.Repository = repo
	p.BuildPath = filepath.Join(repo.SourcePath, p.RelativeBuildPath)
	p.Endpoints = make(map[string]Endpoint)
	for host, ep_string := range p.EndpointStrings {
		components := strings.Split(ep_string, ",")
		if len(components) == 2 {
			p.Endpoints[host] = Endpoint{
				Network:  components[0],
				Address:  components[1],
				HostName: host,
				Host:     hosts.HostByName(host)}
			if p.Endpoints[host].Host == nil {
				return fmt.Errorf("%s -> %s -> %s: Endpoint host %s can't be resolved",
					repo.Host.Name, repo.Name, p.Name, host)
			}
		} else {
			return fmt.Errorf("Address \"%s\" was invalid. Should be of the form <network>,<address>", ep_string)
		}
	}
	return p.Validate()
}

func (p *PlatformConfig) EndpointFor(host *HostConfig) *Endpoint {
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

func (p *PlatformConfig) Validate() error {
	if p.RelativeBuildPath == "" {
		return fmt.Errorf("RelativeBuildPath invalid for %s", p.Name)
	}
	if p.MbConfigName == "" {
		return fmt.Errorf("MbConfigName not defined for %s", p.Name)
	}
	if p.Name == "" || p.Repository == nil || p.BuildPath == "" {
		return fmt.Errorf("Platform not normalized")
	}
	return nil
}

func (p *PlatformConfig) RelativePath(paths ...string) string {
	paths = append([]string{p.BuildPath}, paths...)
	return filepath.Join(paths...)
}

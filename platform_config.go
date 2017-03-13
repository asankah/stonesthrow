package stonesthrow

import (
	"context"
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
	p.Name = name
	p.Repository = repo
	p.BuildPath = filepath.Join(repo.SourcePath, p.RelativeBuildPath)
	p.Endpoints = make(map[string]Endpoint)
	for host, epString := range p.EndpointStrings {
		components := strings.Split(epString, ",")
		if len(components) == 2 {
			p.Endpoints[host] = Endpoint{Network: components[0],
				Address:  components[1],
				HostName: host}
		} else {
			return fmt.Errorf("Address \"%s\" was invalid. Should be of the form <network>,<address>", epString)
		}
	}
	return p.Validate()
}

func (p *PlatformConfig) EndpointFor(host *HostConfig) *Endpoint {
	for _, ep := range p.Endpoints {
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
		return fmt.Errorf("MbConfigName not defiend for %s", p.Name)
	}
	if p.Name == "" || p.Repository == nil || p.BuildPath == "" {
		return fmt.Errorf("Platform not normalized")
	}
	return nil
}

func (p *PlatformConfig) RunHere(ctx context.Context, e Executor, command ...string) (string, error) {
	return e.ExecuteSilently(ctx, p.BuildPath, command...)
}

func (p *PlatformConfig) GetAllTargets(testOnly bool) (map[string]Command, error) {
	return map[string]Command{
		"net_unittests":        {Aliases: []string{"nu"}},
		"content_unittests":    {Aliases: []string{"cu"}},
		"content_browsertests": {Aliases: []string{"cb"}},
		"unit_tests":           {},
		"browser_tests":        {}}, nil
}

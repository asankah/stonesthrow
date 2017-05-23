package stonesthrow

import (
	"fmt"
	"path/filepath"
)

type Endpoint struct {
	Network string
	Address string

	HostName string
	Host     *HostConfig
}

type PlatformConfig struct {
	RelativeBuildPath string `json:"out,omitempty"`
	MbConfigName      string `json:"mb_config,omitempty"`

	Name       string            `json:"-"`
	BuildPath  string            `json:"-"`
	Repository *RepositoryConfig `json:"-"`
}

func (p *PlatformConfig) Normalize(name string, repo *RepositoryConfig) error {
	p.Name = name
	p.Repository = repo
	p.BuildPath = filepath.Join(repo.SourcePath, p.RelativeBuildPath)
	return p.Validate()
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

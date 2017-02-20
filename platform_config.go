package stonesthrow

import (
	"fmt"
	"path/filepath"
	"strings"
)

type PlatformConfig struct {
	FullAddressString string `json:"address"`
	RelativeBuildPath string `json:"out,omitempty"`
	MbConfigName      string `json:"mb_config,omitempty"`

	Name       string            `json:"-"`
	BuildPath  string            `json:"-"`
	Repository *RepositoryConfig `json:"-"`
	Network    string            `json:"-"`
	Address    string            `json:"-"`
}

func (p *PlatformConfig) Normalize(name string, repo *RepositoryConfig) {
	p.Name = name
	p.Repository = repo
	p.BuildPath = filepath.Join(repo.SourcePath, p.RelativeBuildPath)
	components := strings.Split(p.FullAddressString, ",")
	if len(components) == 2 {
		p.Network = components[0]
		p.Address = components[1]
	}
}

func (p *PlatformConfig) Validate() error {
	if p.Name == "" || p.Repository == nil || p.BuildPath == "" {
		return fmt.Errorf("Platform not normalized")
	}
	if p.FullAddressString == "" {
		return fmt.Errorf("Address unspecified for %s", p.Name)
	}
	if p.Network == "" || p.Address == "" {
		return fmt.Errorf("Address %s was invalid. Should be of the form <network>,<address>", p.FullAddressString)
	}
	if p.RelativeBuildPath == "" {
		return fmt.Errorf("RelativeBuildPath invalid for %s", p.Name)
	}
	if p.MbConfigName == "" {
		return fmt.Errorf("MbConfigName not defiend for %s", p.Name)
	}
	return nil
}

func (p *PlatformConfig) RunHere(command ...string) (string, error) {
	return RunCommandWithWorkDir(p.BuildPath, command...)
}

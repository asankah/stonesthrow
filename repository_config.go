package stonesthrow

import (
	"fmt"
	"path/filepath"
)

type RepositoryGitConfig struct {
	SyncableProperties []string    `json:"syncable_properties,omitempty"`
	Remote             string      `json:"remote,omitempty"`
	RemoteHostname     string      `json:"hostname,omitempty"`
	RemoteHost         *HostConfig `json:"-"`
}

type RepositoryConfig struct {
	SourcePath string                     `json:"src"`
	Platforms  map[string]*PlatformConfig `json:"platforms"`
	GitConfig  RepositoryGitConfig        `json:"git"`

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

func (r *RepositoryConfig) AnyPlatform() *PlatformConfig {
	for _, platform := range r.Platforms {
		return platform
	}
	return nil
}

func (r *RepositoryConfig) RelativePath(path string) string {
	return filepath.Join(r.SourcePath, path)
}

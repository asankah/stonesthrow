package stonesthrow

import (
	"fmt"
	"path/filepath"
)

type RepositoryGitConfig struct {
	SyncableProperties []string `json:"syncable_properties,omitempty"`
	Remote             string   `json:"remote,omitempty"`
	RemoteHostname     string   `json:"hostname,omitempty"`

	KnownBranches map[string]string `json:"-"`
	RemoteHost    *HostConfig       `json:"-"`
}

type RepositoryConfig struct {
	SourcePath string                     `json:"src"`
	Platforms  map[string]*PlatformConfig `json:"platforms"`
	GitConfig  RepositoryGitConfig        `json:"git"`

	Name string      `json:"-"`
	Host *HostConfig `json:"-"`
}

func (r *RepositoryConfig) Normalize(name string, host_config *HostConfig) error {
	r.Host = host_config
	r.Name = name

	if r.GitConfig.RemoteHostname != "" {
		r.GitConfig.RemoteHost = host_config.HostsConfig.HostByName(r.GitConfig.RemoteHostname)
		if r.GitConfig.RemoteHost == nil {
			return fmt.Errorf("%s -> %s: git remote %s can't be resolved",
				host_config.Name, r.Name, r.GitConfig.RemoteHostname)
		}
	}

	r.GitConfig.KnownBranches = make(map[string]string)

	for platform_name, platform_config := range r.Platforms {
		err := platform_config.Normalize(platform_name, r)
		if err != nil {
			return err
		}
	}

	global_host := host_config.HostsConfig.HostByName("*")
	if global_host != nil && host_config.Name != "*" {
		template_repo, ok := global_host.Repositories[r.Name]
		if ok {
			if len(r.GitConfig.SyncableProperties) != 0 {
				return fmt.Errorf("%s -> %s: syncable_properties will be overridden by wildcard settings.",
					r.Host.Name, r.Name)
			}
			r.GitConfig.SyncableProperties = template_repo.GitConfig.SyncableProperties
		}
	}
	return r.Validate()
}

func (r *RepositoryConfig) Validate() error {
	if r.Host == nil || r.Name == "" {
		return fmt.Errorf("repositoryConfig not normalized")
	}

	if r.SourcePath == "" && !r.Host.IsWildcard() {
		return fmt.Errorf("sourcePath invalid for %s in %s", r.Name, r.Host.Name)
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
	for _, platform_name := range r.Platforms {
		return platform_name
	}
	return nil
}

func (r *RepositoryConfig) RelativePath(path string) string {
	return filepath.Join(r.SourcePath, path)
}

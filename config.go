package stonesthrow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"text/template"
)

type ConfigurationFile struct {
	FileName    string      // Configuration file where this Config came from.
	HostsConfig HostsConfig // Configuration as read from ConfigurationFile
}

func (c *ConfigurationFile) ReadFrom(filename string) error {
	c.FileName = filename
	c.HostsConfig.ConfigurationFile = c
	return c.HostsConfig.ReadFrom(filename)
}

// Config describes a configuration where a host, repository and platform is
// defined.
type Config struct {
	Host       *HostConfig       // hostconfig for the remote end hosting |Platform|.
	Repository *RepositoryConfig // RepositoryConfig for the remote end.
	Platform   *PlatformConfig   // PlatformConfig for the local end.

	ConfigurationFile *ConfigurationFile // Source
}

func (c *Config) newError(s string, v ...interface{}) error {
	configFile := c.ConfigurationFile.FileName
	if configFile == "" {
		configFile = "<unknown configuration file>"
	}
	return NewConfigurationError("Config file: %s: %s", configFile, fmt.Sprintf(s, v...))
}

func (c *Config) SetFromRepository(repo *RepositoryConfig) {
	c.ConfigurationFile = repo.Host.HostsConfig.ConfigurationFile
	c.Host = repo.Host
	c.Repository = repo
	c.Platform = repo.AnyPlatform()
}

func (c *Config) Set(host *HostConfig, repo *RepositoryConfig, platform *PlatformConfig) {
	c.ConfigurationFile = host.HostsConfig.ConfigurationFile
	c.Host = host
	c.Repository = repo
	c.Platform = platform

	if c.Host != c.Repository.Host ||
		c.Platform.Repository != c.Repository ||
		c.Host.HostsConfig.ConfigurationFile != c.ConfigurationFile {
		panic("Mismatch between host, repository, and platform")
	}
}

func (c *Config) SetFromLocalRepository(configFile *ConfigurationFile, repository string) error {
	err := c.Select(configFile, "", repository, "")
	if err != nil {
		return err
	}

	// Resetting local platform since the local machine is not required to support the target plaform.
	c.Platform = nil
	return nil
}

func (c *Config) getRepositoryNameFromCurrentDir(host_config *HostConfig) (string, error) {
	current_dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	repo_info_map := make(map[string]os.FileInfo)

	for name, config := range host_config.Repositories {
		file_info, err := os.Stat(config.SourcePath)
		if err != nil {
			return "", err
		}
		repo_info_map[name] = file_info
	}

	for {
		file_info, err := os.Stat(current_dir)
		if err != nil {
			return "", err
		}

		for name, repo_info := range repo_info_map {
			if os.SameFile(file_info, repo_info) {
				return name, nil
			}
		}

		current_dir = filepath.Dir(current_dir)
		if current_dir == "." || current_dir == string(filepath.Separator) {
			return "", io.EOF
		}
	}
}

func (c *Config) Select(config_file *ConfigurationFile, host, repository, platform string) error {
	c.ConfigurationFile = config_file

	if repository == "" {
		localhost, err := os.Hostname()
		if err != nil {
			return c.newError("can't determine localhost")
		}
		repository, err = c.getRepositoryNameFromCurrentDir(config_file.HostsConfig.HostByName(localhost))
		if err != nil {
			return c.newError("can't select repository for current directory")
		}
	}

	error_message := fmt.Sprintf("can't determine configuration for host=%s, platform=%s, and repository=%s",
		host, platform, repository)

	if host == "" {
		if repository != "" && platform != "" {
			c.Host = config_file.HostsConfig.HostForPlatform(repository, platform)
		}

		if c.Host == nil {
			localhost, err := os.Hostname()
			if err != nil {
				return c.newError("can't determine localhost: %s", err.Error())
			}
			c.Host = config_file.HostsConfig.HostByName(localhost)
		}
	} else {
		c.Host = config_file.HostsConfig.HostByName(host)
	}

	if c.Host == nil {
		return c.newError(error_message)
	}

	c.Repository, _ = c.Host.Repositories[repository]
	if c.Repository == nil {
		return c.newError(error_message)
	}

	if platform == "" {
		c.Platform = c.Repository.AnyPlatform()
	} else {
		c.Platform, _ = c.Repository.Platforms[platform]
	}

	if c.Platform == nil && (len(c.Repository.Platforms) != 0 || platform != "") {
		return c.newError(error_message)
	}

	if c.Platform == nil {
		c.Platform = &PlatformConfig{Repository: c.Repository}
	}

	return nil
}

// GetSourcePath returns the server's source path. Arguments are treated as
// path components relative to the source path.
func (c *Config) GetSourcePath(p ...string) string {
	paths := append([]string{c.Repository.SourcePath}, p...)
	return filepath.Join(paths...)
}

// GetBuildPath returns the server's build path. Arguments are treated as path
// components relative to the build path.
func (c *Config) GetBuildPath(p ...string) string {
	paths := append([]string{c.Platform.BuildPath}, p...)
	return filepath.Join(paths...)
}

func (c *Config) IsValid() bool {
	return c.ConfigurationFile != nil &&
		c.Repository != nil &&
		c.Host != nil
	// c.Platform is optional
}

func (c *Config) Dump(writer io.Writer) {
	t := template.New("s")
	_, err := t.Parse(`
{{if .PlatformName}}Platform         : {{.PlatformName}}{{end}}
Repository       : {{.RepositoryName}}
{{if .ConfigurationFile}}ConfigurationFile: {{.ConfigurationFile.FileName}}{{end}}

Host :{{with .Host}}
  Name        : {{.Name}}
  GomaPath    : {{.GomaPath}}
  Stonesthrow : {{.StonesthrowPath}}
  MaxBuildJobs: {{.MaxBuildJobs}}
{{if .DefaultRepository}}  Default Repo: {{.DefaultRepository.Name}}{{end}}
{{if .SshTargets}}
  SSH Targets :{{range .SshTargets}}
    Hostname  : {{.HostName}}{{if .Host}} [Resolved]{{end}}
    SSH Host  : {{.SshHostName}}
{{end}}
{{end}}{{end}}
Repository:{{with .Repository}}
  Name          : {{.Name}}
  SourcePath    : {{.SourcePath}}
  GitRemote     : {{.GitRemote}}
  MasterHostname: {{.MasterHostname}}
{{end}}
{{if .Platform}}Platform:{{with .Platform}}
  Name        : {{.Name}}
  BuildPath   : {{.BuildPath}}
  Network     : {{.Network}}
  Address     : {{.Address}}
  MbConfigName: {{.MbConfigName}}
{{if .Endpoints}}
  Endpoints   :{{range .Endpoints}}
    Address   : {{.Address}} [{{.Network}}] on {{.HostName}}{{if .Host}} [Resolved]{{end}}{{end}}
{{end}}{{end}}{{end}}
`)
	if err != nil {
		fmt.Fprint(writer, err.Error())
		return
	}
	t.Execute(writer, c)
}

// GetDefaultConfigFileName() returns the platform specific default configuration file path.
func GetDefaultConfigFileName() string {
	if runtime.GOOS == "windows" {
		return os.ExpandEnv("${APPDATA}\\StonesThrow.cfg")
	}
	return os.ExpandEnv("${HOME}/.stonesthrow")
}

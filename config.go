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
	return c.HostsConfig.ReadFrom(filename)
}

type Config struct {
	PlatformName   string // Platform string.
	RepositoryName string // Repository.

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
	return ConfigError{ConfigFile: configFile, ErrorString: fmt.Sprintf(s, v...)}
}

func (c *Config) SelectClientConfig(configFile *ConfigurationFile, serverPlatform string, repository string) error {
	err := c.SelectServerConfig(configFile, serverPlatform, repository)
	if err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	var ok bool
	c.Host, ok = configFile.HostsConfig.Hosts[hostname]
	if !ok {
		return c.newError("Can't determine local host config for %s", hostname)
	}

	c.Repository, ok = c.Host.Repositories[c.RepositoryName]
	if !ok {
		return c.newError("Can't determine local repository.")
	}

	// Resetting local platform since the local machine is not required to support the target plaform.
	c.Platform = nil
	c.PlatformName = ""
	return nil
}

// ReadServerConfig reads the configuration from |filename| and populates the
// receiver with the values corresponding to |platform| and |repository|.  It
// returns an error if something went wrong, in which case the state of the
// receiver is unknown.
func (c *Config) SelectServerConfig(configFile *ConfigurationFile, platform string, repository string) error {
	c.ConfigurationFile = configFile

	c.PlatformName = platform

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	c.Host = configFile.HostsConfig.HostForPlatform(platform, hostname)
	if c.Host == nil {
		return fmt.Errorf("%s is not a valid platform", c.PlatformName)
	}

	var ok bool
	if repository == "" {
		for name, config := range c.Host.Repositories {
			c.Platform, ok = config.Platforms[platform]
			if ok {
				repository = name
				c.Repository = config
				break
			}
		}
	} else {
		c.Repository, ok = c.Host.Repositories[repository]
		if ok {
			c.Platform, ok = c.Repository.Platforms[platform]
		}
	}

	if c.Host == nil || c.Repository == nil || c.Platform == nil {
		return c.newError("Can't determine configuration for platform=%s and repository=%s", platform, repository)
	}
	c.RepositoryName = repository

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
		c.RepositoryName != "" &&
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

// GetDefaultConfigFile() returns the platform specific default configuration file path.
func GetDefaultConfigFile() string {
	if runtime.GOOS == "windows" {
		return os.ExpandEnv("${APPDATA}\\StonesThrow.cfg")
	}
	return os.ExpandEnv("${HOME}/.stonesthrow")
}

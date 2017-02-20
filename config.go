package stonesthrow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"text/template"
)

type Config struct {
	PlatformName   string // Platform string.
	RepositoryName string // Repository.

	Host       *HostConfig       // hostconfig for the remote end hosting |Platform|.
	Repository *RepositoryConfig // RepositoryConfig for the remote end.
	Platform   *PlatformConfig   // PlatformConfig for the local end.

	ConfigurationFile string      // Configuration file where this Config came from.
	HostsConfig       HostsConfig // Configuration as read from ConfigurationFile
}

func (c *Config) newError(s string, v ...interface{}) error {
	configFile := c.ConfigurationFile
	if configFile == "" {
		configFile = "<unknown configuration file>"
	}
	return ConfigError{ConfigFile: configFile, ErrorString: fmt.Sprintf(s, v...)}
}

func (c *Config) ReadClientConfig(filename, serverPlatform string, repository string) error {
	err := c.ReadServerConfig(filename, serverPlatform, repository)
	if err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	var ok bool
	c.Host, ok = c.HostsConfig.Hosts[hostname]
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
func (c *Config) ReadServerConfig(filename, platform string, repository string) error {
	c.ConfigurationFile = filename
	err := c.HostsConfig.ReadFrom(filename)
	if err != nil {
		return err
	}

	c.PlatformName = platform
	var ok bool
	c.Host, ok = c.HostsConfig.PlatformHostMap[platform]
	if !ok {
		return fmt.Errorf("%s is not a valid platform", c.PlatformName)
	}

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
	return c.ConfigurationFile != "" &&
		c.PlatformName != "" &&
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
ConfigurationFile: {{.ConfigurationFile}}

Host :{{with .Host}}
  Name        : {{.Name}}
  GomaPath    : {{.GomaPath}}
  MaxBuildJobs: {{.MaxBuildJobs}}
{{end}}
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
{{end}}{{end}}
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

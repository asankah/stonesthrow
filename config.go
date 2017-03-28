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
	return NewConfigurationError("Config file: %s: %s", configFile, fmt.Sprintf(s, v...))
}

func (c *Config) SelectLocalClientConfig(configFile *ConfigurationFile, serverPlatform string, repository string) error {
	err := c.SelectServerConfig(configFile, serverPlatform, repository)
	if err != nil {
		return err
	}

	localhost, err := os.Hostname()
	if err != nil {
		return err
	}

	var ok bool
	c.Host, ok = configFile.HostsConfig.Hosts[localhost]
	if !ok {
		return c.newError("Can't determine local host config for %s", localhost)
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

func (c *Config) selectRepositoryFromCurrentDir(localhost string) (string, error) {
	current_dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	host_config, ok := c.ConfigurationFile.HostsConfig.Hosts[localhost]
	if !ok {
		return "", c.newError("localhost (%s) doesn't have a mapping.", localhost)
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

// ReadServerConfig reads the configuration from |filename| and populates the
// receiver with the values corresponding to |platform| and |repository|.  It
// returns an error if something went wrong, in which case the state of the
// receiver is unknown.
func (c *Config) SelectServerConfig(configFile *ConfigurationFile, platform string, repository string) error {
	c.ConfigurationFile = configFile
	c.PlatformName = platform

	localhost, err := os.Hostname()
	if err != nil {
		return err
	}

	if repository == "" {
		var err error
		repository, err = c.selectRepositoryFromCurrentDir(localhost)
		if err != nil {
			return c.newError("can't select repository for current directory")
		}
	}

	c.Host = configFile.HostsConfig.HostForPlatform(repository, platform, localhost)
	if c.Host == nil {
		return fmt.Errorf("%s is not a valid platform", c.PlatformName)
	}

	var ok bool
	c.Repository, ok = c.Host.Repositories[repository]
	if ok {
		c.Platform, _ = c.Repository.Platforms[platform]
	}

	if c.Host == nil || c.Repository == nil || c.Platform == nil {
		return c.newError("Can't determine configuration for platform=%s and repository=%s", platform, repository)
	}
	c.RepositoryName = repository

	return nil
}

func (c *Config) SelectPeerConfig(configFile *ConfigurationFile, hostname string, repository string) error {
	c.ConfigurationFile = configFile
	var ok bool
	c.Host, ok = configFile.HostsConfig.Hosts[hostname]
	if !ok {
		return c.newError("Hostname %s cannot be resolved using %s", hostname, configFile.FileName)
	}

	c.Repository, ok = c.Host.Repositories[repository]
	if !ok {
		return c.newError("Repository %s cannot be resolved using %s", repository, configFile.FileName)
	}

	c.RepositoryName = c.Repository.Name

	// Unlike a client configuration, a peer configuration uses a non-empty
	// platform. It is expected that any platform will do since the peer
	// configuration is only used to access the underlying repository. The
	// plaform is only used as a means of locating an endpoint.
	c.Platform = c.Repository.AnyPlatform()
	if c.Platform == nil {
		return c.newError("Peer repository for %s on %s doesn't have a usable platform.", repository, hostname)
	}
	c.PlatformName = c.Platform.Name
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

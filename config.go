package stonesthrow

import (
	"bufio"
	"encoding/json"

	"io"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"text/template"
	"strings"
)

type ConfigError struct {
	ConfigFile  string
	ErrorString string
}

func (c ConfigError) Error() string {
	return fmt.Sprintf("Configuration error: %s: %s", c.ConfigFile, c.ErrorString)
}

type PlatformConfig struct {
	FullAddressString           string `json:"address"`
	RelativeBuildPath string `json:"out,omitempty"`
	MbConfigName      string `json:"mb_config,omitempty"`

	Name       string            `json:"-"`
	BuildPath  string            `json:"-"`
	Repository *RepositoryConfig `json:"-"`
	Network string `json:"-"`
	Address string `json:"-"`

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

type RepositoryConfig struct {
	SourcePath     string                     `json:"src"`
	GitRemote      string                     `json:"git_remote,omitempty"`
	MasterHostname string                     `json:"git_hostname"`
	Platforms      map[string]*PlatformConfig `json:"platforms"`

	Name string      `json:"-"`
	Host *HostConfig `json:"-"`
}

func (r *RepositoryConfig) Normalize(name string, hostConfig *HostConfig) {
	r.Host = hostConfig
	r.Name = name

	for platform, platformConfig := range r.Platforms {
		platformConfig.Normalize(platform, r)
	}
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

func (r *RepositoryConfig) RunHere(command ...string) (string, error) {
	return RunCommandWithWorkDir(r.SourcePath, command...)
}

func (r *RepositoryConfig) GitRevision(name string) (string, error) {
	return r.RunHere("git", "rev-parse", name)
}

func (r *RepositoryConfig) GitHasUnmergedChanges() bool {
	gitStatus, err := r.RunHere("git", "status", "--porcelain=2",
		"--untracked-files=no", "--ignore-submodules")
	if err != nil {
		return false
	}

	scanner := bufio.NewScanner(strings.NewReader(gitStatus))
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "u ") {
			return true
		}
	}

	return false
}

func (r *RepositoryConfig) GitCreateWorkTree() (string, error) {
	if r.GitHasUnmergedChanges() {
		return "", UnmergedChangesExistError
	}

	_, err := r.RunHere("git", "add", "-u")
	if err != nil {
		return "", err
	}
	return r.RunHere("git", "write-tree")
}

type HostConfig struct {
	Alias        []string                     `json:"alias,omitempty"`
	Repositories map[string]*RepositoryConfig `json:"repositories,omitempty"`
	GomaPath     string                       `json:"goma_path,omitempty"`
	MaxBuildJobs int                          `json:"max_build_jobs,omitempty"`

	Name              string            `json:"-"`
	DefaultRepository *RepositoryConfig `json:"-"`
}

func (h *HostConfig) Normalize(hostname string) {
	h.Name = hostname
	for repository, repositoryConfig := range h.Repositories {
		repositoryConfig.Normalize(repository, h)
		h.DefaultRepository = repositoryConfig
	}
}

func (h *HostConfig) Validate() error {
	if h.DefaultRepository == nil || h.Name == "" {
		return fmt.Errorf("not normalized or no repositories")
	}

	for _, r := range h.Repositories {
		err := r.Validate()
		if err != nil {
			return err
		}
	}
	return nil
}

// HostConfig is the on-disk format for configuring Stonesthrow.
type HostsConfig struct {
	Hosts map[string]*HostConfig `json:"hosts"`

	// Maps from a platform to a HostConfig. Each platform is restricted to
	// a single host, but can support multiple repositories.
	PlatformHostMap map[string]*HostConfig `json:"-"`
}

func (h *HostsConfig) Normalize() {
	if h.PlatformHostMap == nil {
		h.PlatformHostMap = make(map[string]*HostConfig)
	}

	for hostName, hostConfig := range h.Hosts {
		hostConfig.Normalize(hostName)
		for _, alias := range hostConfig.Alias {
			h.Hosts[alias] = hostConfig
		}

		for _, repo := range hostConfig.Repositories {
			for platform, _ := range repo.Platforms {
				h.PlatformHostMap[platform] = hostConfig
			}
		}
	}
}

func (h *HostsConfig) Validate() error {
	if h.PlatformHostMap == nil {
		return fmt.Errorf("HostsConfig not normalized")
	}

	for _, host := range h.Hosts {
		err := host.Validate()
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *HostsConfig) ReadFrom(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("Can't read configuration file %s : %s", filename, err.Error())
	}

	err = json.Unmarshal(data, &h.Hosts)
	if err != nil {
		return fmt.Errorf("Can't read configuration file %s : %s", filename, err.Error())
	}

	if h.Hosts == nil || len(h.Hosts) == 0 {
		return fmt.Errorf("No configuration entries found in %s.", filename)
	}

	h.Normalize()
	return h.Validate()
}

type Config struct {
	PlatformName   string // Platform string.
	RepositoryName string // Repository.

	Host        *HostConfig       // hostconfig for the remote end hosting |Platform|.
	Repository *RepositoryConfig // RepositoryConfig for the remote end.
	Platform    *PlatformConfig   // PlatformConfig for the local end.

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
Platform         : {{.PlatformName}}
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
Platform:{{with .Platform}}
  Name        : {{.Name}}
  BuildPath   : {{.BuildPath}}
  Network     : {{.Network}}
  Address     : {{.Address}}
  MbConfigName: {{.MbConfigName}}
{{end}}`)
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

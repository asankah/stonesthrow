package stonesthrow

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type ConfigError struct {
	ConfigFile string
	ErrorString string
}

func (c ConfigError) Error() string {
	return fmt.Sprintf("Configuration error: %s: %s", c.ConfigFile, c.ErrorString)
}

type Config struct {
	Hostname          string // Hostname
	SourcePath        string // Full path to Chromium source directory.
	GomaPath          string // Path to Goma installation. Should be the directory containing goma_ctl.
	BuildPath         string // Full path to build directory.
	ServerPort        int    // TCP port for listening to client connections.
	RelativeBuildPath string // Build path relative to SourcePath.
	MbConfigName      string // MultiBuild configuration name. See //src/tools/mb
	Platform          string // Platform string. Should be one of the supported platforms.
	MasterHostname    string // Hostname of master host running sshd.
	GitRemote         string // Remote master name as known to Git.
	MaxBuildJobs      int    // Maximum number of jobs to use. Set to 0 to use default.
	ConfigurationFile string // Configuration file where this Config came from.
}

func (c *Config) newError(s string, v ...interface{}) error {
	configFile := c.ConfigurationFile
	if configFile == "" {
		configFile = "<unknown configuration file>"
	}
	return ConfigError{ ConfigFile: configFile, ErrorString: fmt.Sprintf(s, v...)}
}

func (c *Config) IsValid() bool {
	return c.ServerPort != 0 &&
		c.Platform != "" &&
		c.MbConfigName != "" &&
		c.SourcePath != "" &&
		c.BuildPath != "" &&
		c.Hostname != "" &&
		c.MasterHostname != "" &&
		(c.IsMaster() || c.GitRemote != "")
}

func (c *Config) IsMaster() bool {
	return c.Hostname != "" && c.Hostname == c.MasterHostname
}

func (c *Config) GetListenAddress() string {
	return fmt.Sprintf("127.0.0.1:%d", c.ServerPort)
}

func (c *Config) GetPort() int {
	return c.ServerPort
}

func (c *Config) GetDefaultPlatform(configMap map[string]Config) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	hostConfig, ok := configMap[hostname]
	if ok && hostConfig.Platform != "" {
		return hostConfig.Platform, nil
	}
	if ok && hostConfig.Hostname != "" {
		hostConfig, ok := configMap[c.Hostname]
		if ok && hostConfig.Platform != "" {
			return hostConfig.Platform, nil
		}
	}

	return "", c.newError("Can't determine default platform. Tried hostname %s", hostname)
}

func (c *Config) ReadFrom(filename, platform string) error {
	c.ConfigurationFile = filename
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("Can't read configuration file %s : %s", filename, err.Error())
	}

	configs := make(map[string]Config)
	err = json.Unmarshal(data, &configs)
	if err != nil {
		return fmt.Errorf("Can't read configuration file %s : %s", filename, err.Error())
	}

	masterConfig, hasKey := configs["master"]
	if !hasKey {
		return c.newError("No 'master' entry.")
	}
	c.MasterHostname = masterConfig.Hostname

	if platform == "" {
		platform, err = c.GetDefaultPlatform(configs)
		if err != nil {
			return err
		}
	}

	platformConfig, hasKey := configs[platform]
	if !hasKey {
		return c.newError("Unknown platform : %s", platform)
	}

	c.MergeFrom(&platformConfig)
	c.Platform = platform

	hostConfig, hasKey := configs[c.Hostname]
	if !hasKey {
		return c.newError("Unknown Hostname : %s", c.Hostname)
	}
	c.MergeFrom(&hostConfig)

	if c.SourcePath == "" {
		return c.newError("Source path is empty")
	}
	if c.MbConfigName == "" {
		return c.newError("MB config missing")
	}
	if c.ServerPort == 0 {
		return c.newError("No server port")
	}
	if c.RelativeBuildPath == "" {
		c.RelativeBuildPath = filepath.Join("out", fmt.Sprintf("%s-gn", platform))
	}
	if c.BuildPath == "" {
		c.BuildPath = filepath.Join(c.SourcePath, c.RelativeBuildPath)
	}
	if !c.IsValid() {
		return c.newError("Required fields missing")
	}
	return nil
}

func (c *Config) MergeFrom(other *Config) {
	if other.Hostname != "" {
		c.Hostname = other.Hostname
	}

	if other.SourcePath != "" {
		c.SourcePath = other.SourcePath
	}

	if other.GomaPath != "" {
		c.GomaPath = other.GomaPath
	}

	if other.BuildPath != "" {
		c.BuildPath = other.BuildPath
	}

	if other.ServerPort != 0 {
		c.ServerPort = other.ServerPort
	}

	if other.RelativeBuildPath != "" {
		c.RelativeBuildPath = other.RelativeBuildPath
	}

	if other.MbConfigName != "" {
		c.MbConfigName = other.MbConfigName
	}
	
	if other.GitRemote != "" {
		c.GitRemote = other.GitRemote
	}

	if other.MasterHostname != "" {
		c.MasterHostname = other.MasterHostname
	}

	if other.MaxBuildJobs != 0 {
		c.MaxBuildJobs = other.MaxBuildJobs
	}
}

func (c *Config) GetSourcePath(p ...string) string {
	paths := append([]string{c.SourcePath}, p...)
	return filepath.Join(paths...)
}

func (c *Config) GetBuildPath(p ...string) string {
	paths := append([]string{c.BuildPath}, p...)
	return filepath.Join(paths...)
}

func (c *Config) RunInSourceDir(command ...string) (string, error) {
	return RunCommandWithWorkDir(c.SourcePath, command...)
}

func (c *Config) RunInBuildDir(command ...string) (string, error) {
	return RunCommandWithWorkDir(c.BuildPath, command...)
}

func (c *Config) GitGetRevision(name string) (string, error) {
	return c.RunInSourceDir("git", "rev-parse", name)
}

func (c *Config) GitGetTreeFromRevision(revision string) (string, error) {
	return c.RunInSourceDir("git", "rev-parse", fmt.Sprintf("%s^{tree}", revision))
}

func (c *Config) GitHasUnmergedChanges() bool {
	gitStatus, err := c.RunInSourceDir("git", "status", "--porcelain=2",
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

func (c *Config) GitGetEffectiveWorkTree() (string, error) {
	if c.GitHasUnmergedChanges() {
		return "", UnmergedChangesExistError
	}

	c.RunInSourceDir("git", "add", "-u")
	return c.RunInSourceDir("git", "write-tree")
}

func (c *Config) String() string {
	j, err := json.Marshal(c)
	if err != nil {
		return ""
	}

	var o bytes.Buffer
	json.Indent(&o, j, "", "\t")
	return string(o.Bytes())
}

func GetDefaultConfigFile() string {
	if runtime.GOOS == "windows" {
		return os.ExpandEnv("${APPDATA}\\StonesThrow.cfg")
	}
	return os.ExpandEnv("${HOME}/.stonesthrow")
}

func GetPackageRootPath() (string, error) {
	goPath := os.Getenv("GOPATH")
	packagePath := filepath.Join(goPath, "src", "stonesthrow")
	_, err := os.Stat(packagePath)
	if err != nil {
		return "", nil
	}

	return packagePath, nil
}

package stonesthrow

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

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
		if hostConfig.Name == "" {
			hostConfig.Normalize(hostName)
		}
		for _, alias := range hostConfig.Alias {
			h.Hosts[alias] = hostConfig
		}

		for _, repo := range hostConfig.Repositories {
			for platform := range repo.Platforms {
				h.PlatformHostMap[platform] = hostConfig
			}
		}
	}

	// Resolve SSH config references. We are doing this separately because
	// we want all the aliases to be resolved before we start looking at
	// SSH configs.
	for _, hostConfig := range h.Hosts {
		for index, remote := range hostConfig.SshTargets {
			hostConfig.SshTargets[index].Host, _ = h.Hosts[remote.HostName]
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

package stonesthrow

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

// HostConfig is the on-disk format for configuring Stonesthrow.
type HostsConfig struct {
	Hosts map[string]*HostConfig `json:"hosts"`
}

func (h *HostsConfig) Normalize() error {
	for _, hostConfig := range h.Hosts {
		for _, alias := range hostConfig.Alias {
			h.Hosts[alias] = hostConfig
		}
	}

	for hostName, hostConfig := range h.Hosts {
		if hostConfig.Name != "" {
			// No need to do this again if we've normalized this HostConfig
			continue
		}

		for remote_host, remote := range hostConfig.Remotes {
			remote.HostName = remote_host
			remote.Host, _ = h.Hosts[remote_host]
		}

		err := hostConfig.Normalize(hostName)
		if err != nil {
			return err
		}

		for _, repo := range hostConfig.Repositories {
			if repo.GitConfig.RemoteHostname != "" {
				var ok bool
				repo.GitConfig.RemoteHost, ok = h.Hosts[repo.GitConfig.RemoteHostname]
				if !ok {
					return fmt.Errorf("%s -> %s: Git remote %s can't be resolved",
						hostConfig.Name, repo.Name, repo.GitConfig.RemoteHostname)
				}
			}
			for _, platform := range repo.Platforms {
				for hostName, ep := range platform.Endpoints {
					var ok bool
					ep.Host, ok = h.Hosts[ep.HostName]
					if !ok {
						return fmt.Errorf("%s -> %s -> %s: Endpoint host %s can't be resolved",
							hostConfig.Name, repo.Name, platform.Name, ep.HostName)
					}
					platform.Endpoints[hostName] = ep
				}
			}
		}
	}

	return h.Validate()
}

func (h *HostsConfig) Validate() error {
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

	return h.Normalize()
}

func (h *HostsConfig) HostForPlatform(platform string, localhost string) *HostConfig {
	config, ok := h.Hosts[localhost]
	if ok && config.SupportsPlatform(platform) {
		return config
	}

	for _, config = range h.Hosts {
		if config.SupportsPlatform(platform) {
			return config
		}
	}

	return nil
}

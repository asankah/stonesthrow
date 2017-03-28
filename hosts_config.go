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
	for host_name, host_config := range h.Hosts {
		if host_config.Name != "" {
			continue
		}
		host_config.Name = host_name
		for _, alias := range host_config.Alias {
			existing_host, ok := h.Hosts[alias]
			if ok && existing_host == host_config {
				return fmt.Errorf("Alias %s is not unique. It's specified twice in %s", alias, existing_host.Name)
			}
			if ok {
				return fmt.Errorf("Alias %s is not unique. It's already assigned to %s", alias, existing_host.Name)
			}
			h.Hosts[alias] = host_config
		}
	}

	for _, host_config := range h.Hosts {
		err := host_config.Normalize(h)
		if err != nil {
			return err
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

func (h *HostsConfig) HostForPlatform(repository string, platform string, localhost string) *HostConfig {
	config, ok := h.Hosts[localhost]
	if ok && config.SupportsPlatform(platform) {
		return config
	}

	for _, config = range h.Hosts {
		repo, ok := config.Repositories[repository]
		if !ok {
			continue
		}

		_, ok = repo.Platforms[platform]
		if ok {
			return config
		}
	}

	return nil
}

func (h *HostsConfig) HostByName(host string) *HostConfig {
	config, ok := h.Hosts[host]
	if ok {
		return config
	}
	return nil
}

func (h *HostsConfig) ShortHost(host string) string {
	config := h.HostByName(host)
	if config == nil || len(config.Alias) == 0 {
		return host
	}
	return config.Alias[0]
}

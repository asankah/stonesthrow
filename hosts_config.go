package stonesthrow

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

// HostConfig is the on-disk format for configuring Stonesthrow.
type HostsConfig struct {
	Hosts             map[string]*HostConfig `json:"hosts"`
	ConfigurationFile *ConfigurationFile
}

func (h *HostsConfig) Normalize() error {
	for host_name, host_config := range h.Hosts {
		if host_config.Name != "" {
			continue
		}
		host_config.Name = host_name
		for _, nickname := range host_config.Nickname {
			existing_host, ok := h.Hosts[nickname]
			if ok && existing_host == host_config {
				return fmt.Errorf("Nickname %s is not unique. It's specified twice in %s", nickname, existing_host.Name)
			}
			if ok {
				return fmt.Errorf("Nickname %s is not unique. It's already assigned to %s", nickname, existing_host.Name)
			}
			h.Hosts[nickname] = host_config
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

func (h *HostsConfig) HostForPlatform(repository string, platform string) *HostConfig {
	for _, config := range h.Hosts {
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
	if config == nil || len(config.Nickname) == 0 {
		return host
	}
	return config.Nickname[0]
}

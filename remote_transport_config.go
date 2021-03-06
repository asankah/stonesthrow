package stonesthrow

import (
	"fmt"
)

type RemoteTransportConfig struct {
	SshHost    string   `json:"ssh_config,omitempty"`
	SshCommand []string `json:"ssh_command,omitempty"`

	HostName string      `json:"-"`
	Host     *HostConfig `json:"-"`
}

func (r *RemoteTransportConfig) Validate() error {
	if r.Host == nil || r.HostName == "" {
		fmt.Printf("%#v", r)
		return fmt.Errorf("not normalized or host is unknown")
	}

	if r.SshHost == "" && r.SshCommand == nil {
		fmt.Printf("%#v", r)
		return fmt.Errorf("no remote connection specified")
	}
	return nil
}

func (r *RemoteTransportConfig) GetSshPassthroughCommand(server *Config) []string {
	command := []string{}

	if r.SshCommand != nil {
		command = r.SshCommand
	} else {
		command = []string{"ssh", r.SshHost}
	}

	return append(command, "-T",
		fmt.Sprintf("%s/bin/st_client", r.Host.GoPath),
		"--platform", server.Platform.Name,
		"--repository", server.Repository.Name,
		"passthrough")
}

package stonesthrow

import (
	"time"
)

type InfoMessage struct {
	Info string `json:"info"`
}

type ErrorMessage struct {
	Error string `json:"error"`
}

type BeginCommandMessage struct {
	IsInteractive bool     `json:"interactive"`
	Command       []string `json:"command"`
	WorkDir       string   `json:"workdir,omitempty"`
	Hostname      string   `json:"host,omitempty"`
}

type TerminalOutputMessage struct {
	Output string `json:"output"`
}

type EndCommandMessage struct {
	ReturnCode   int   `json:"return_code"`
	SystemTimeNs int64 `json:"system_time_ns"`
	UserTimeNs   int64 `json:"user_time_ns"`
}

type Command struct {
	Aliases  []string `json:"aliases,omitempty"`
	Synopsis string   `json:"synposis,omitempty"`
	Usage    string   `json:"usage,omitempty"`
}

type Repository struct {
	Revistion  string `json:"revision,omitempty"`
	SourcePath string `json:"directory,omitempty"`
	BuildPath  string `json:"build_dir,omitempty"`
}

type CommandListMessage struct {
	Synposis     string                `json:"synopsis"`
	ConfigFile   string                `json:"config,omitempty"`
	Commands     map[string]Command    `json:"commands"`
	Repositories map[string]Repository `json:"repositories"`
}

type JobRecord struct {
	Id        int            `json:"id"`
	Request   RequestMessage `json:"request"`
	StartTime time.Time      `json:"start"`
	Duration  time.Duration  `json:"duration"`
	Processes []Process      `json:"processes"`
}

type JobListMessage struct {
	Jobs []JobRecord `json:"jobs"`
}

type Process struct {
	Command    []string      `json:"command"`
	StartTime  time.Time     `json:"start"`
	Duration   time.Duration `json:"duration"`
	Running    bool          `json:"running"`
	EndTime    time.Time     `json:"end"`
	SystemTime time.Duration `json:"system_time"`
	UserTime   time.Duration `json:"user_time"`
}

type ProcessListMessage struct {
	Processes []Process `json:"processes"`
}

type BranchConfig struct {
	Name      string            `json:"name"`
	Revision  string            `json:"revision"`
	GitConfig map[string]string `json:"config"`
}

type RequestMessage struct {
	Command        string         `json:"cmd"`
	Arguments      []string       `json:"args,omitempty"`
	Repository     string         `json:"repo,omitempty"`
	Revision       string         `json:"revision,omitempty"`
	SourceHostname string         `json:"source_hostname,omitempty"`
	BranchConfigs  []BranchConfig `json:"branch_config,omitempty"`
}

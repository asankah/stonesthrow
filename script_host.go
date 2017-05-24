package stonesthrow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
)

type ScriptHostRunner struct {
	Config       Config
	Script       Script
	ProcessAdder ProcessAdder
}

type PlatformBuildPassthroughConfig struct {
	PlatformConfig
	SourcePath     string `json:"source_path"`
	BuildPath      string `json:"build_path"`
	PlatformName   string `json:"platform_name"`
	RepositoryName string `json:"repository_name"`
	GomaPath       string `json:"goma_path"`
	MaxBuildJobs   int    `json:"max_build_jobs"`
}

func (p PlatformBuildPassthroughConfig) AsJson() string {
	bytes, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func (h ScriptHostRunner) GetScript() (*Script, error) {
	if h.Script.ScriptName != "" {
		return &h.Script, nil
	}

	r := h.GetTokenReplacer()
	config := h.Config
	script_path := r.Replace(config.Repository.ScriptPath)
	if script_path == "" {
		return nil, NewInvalidArgumentError("script path not defined")
	}

	h.Script = Script{
		ScriptPath:      filepath.Dir(script_path),
		ScriptName:      filepath.Base(script_path),
		StonesthrowPath: config.Host.StonesthrowPath,
		Config: PlatformBuildPassthroughConfig{
			PlatformConfig: *config.Platform,
			SourcePath:     config.Repository.SourcePath,
			BuildPath:      config.Platform.BuildPath,
			PlatformName:   config.Platform.Name,
			RepositoryName: config.Repository.Name,
			GomaPath:       config.Host.GomaPath,
			MaxBuildJobs:   config.Host.MaxBuildJobs}}

	return &h.Script, nil
}

func (h ScriptHostRunner) GetScriptRunner(e Executor, s JobEventSender) (*ScriptRunner, error) {
	script, err := h.GetScript()
	if err != nil {
		return nil, err
	}
	script_runner := script.GetScriptRunner(e)
	return &script_runner, nil
}

func (h ScriptHostRunner) GetTokenReplacer() *strings.Replacer {
	return strings.NewReplacer(
		"{src}", h.Config.Repository.SourcePath,
		"{out}", h.Config.Platform.BuildPath,
		"{st}", h.Config.Host.StonesthrowPath)
}
func (h ScriptHostRunner) ExpandTokens(in string) string {
	return h.GetTokenReplacer().Replace(in)
}

func (h ScriptHostRunner) ExpandTokensInArray(in []string) []string {
	r := h.GetTokenReplacer()
	out := []string{}
	for _, s := range in {
		out = append(out, r.Replace(s))
	}
	return out
}

func (h ScriptHostRunner) OnRepositoryCheckout(ctx context.Context, e Executor, s JobEventSender) error {
	runner, err := h.GetScriptRunner(e, s)
	if err != nil {
		return err
	}

	return runner.OnRepositoryCheckout(ctx)
}

func (h ScriptHostRunner) ListScriptCommands(ctx context.Context, e Executor) (*CommandList, error) {
	runner, err := h.GetScriptRunner(e, nil)
	if err != nil {
		return nil, err
	}

	command_list, err := runner.ListCommands(ctx)
	if err != nil {
		return nil, err
	}

	return command_list, nil
}

func (h ScriptHostRunner) RunScriptCommand(ro *RunOptions, e Executor, s JobEventServer) error {
	if len(ro.GetCommand().GetCommand()) == 0 {
		return NewInvalidArgumentError("no arguments specified for command")
	}

	runner, err := h.GetScriptRunner(e, s)
	if err != nil {
		return err
	}

	needs_source, err := runner.NeedsSource(s.Context(), ro.GetCommand().GetCommand()...)
	if err != nil {
		return err
	}

	if needs_source {
		repository_host := RepositoryHostServerImpl{Host: h.Config.Host, ProcessAdder: h.ProcessAdder}
		repository_state := RepositoryState{Repository: ro.Repository, Revision: ro.Revision}
		err = repository_host.SyncRemote(&repository_state, s)
		if err != nil {
			return err
		}
	}

	return runner.ExecuteInWorkDirPassthrough(
		h.ExpandTokens(ro.GetCommand().GetDirectory()),
		s.Context(),
		h.ExpandTokensInArray(ro.GetCommand().GetCommand())...)
}

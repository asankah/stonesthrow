package stonesthrow

import (
	"encoding/json"
	"golang.org/x/net/context"
)

type BuildHostServerImpl struct {
	Config       Config
	ProcessAdder ProcessAdder
	Script       Script
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

func (p *BuildHostServerImpl) GetExecutor(s JobEventSender) Executor {
	return NewJobEventExecutor(p.Config.Host.Name, p.Config.Platform.BuildPath, p.ProcessAdder, s)
}

func (p *BuildHostServerImpl) GetRepositoryHostServer() RepositoryHostServer {
	return &RepositoryHostServerImpl{Repository: p.Config.Repository, ProcessAdder: p.ProcessAdder}
}

func (p *BuildHostServerImpl) GetScriptHostRunner() ScriptHostRunner {
	return ScriptHostRunner{Host: p}
}

func (p *BuildHostServerImpl) GetConfig() *Config {
	return &p.Config
}

func (p *BuildHostServerImpl) RunScriptCommand(ro *RunOptions, s BuildHost_RunScriptCommandServer) error {
	return p.GetScriptHostRunner().RunScriptCommand(ro, p.GetExecutor(s), s)
}

func (p *BuildHostServerImpl) ListScriptCommands(
	ctx context.Context, _ *ListCommandsOptions) (*CommandList, error) {
	return p.GetScriptHostRunner().ListScriptCommands(ctx, p.GetExecutor(nil))
}

func (p *BuildHostServerImpl) ListTargets(context.Context, *ListTargetsOptions) (*TargetList, error) {
	return nil, NewNothingToDoError("not implemented")
}

func (p *BuildHostServerImpl) RunShellCommand(ro *RunOptions, s BuildHost_RunShellCommandServer) error {
	e := p.GetExecutor(s)
	return e.ExecuteInWorkDirPassthrough(
		p.GetScriptHostRunner().ExpandTokens(ro.GetCommand().GetDirectory()),
		s.Context(),
		p.GetScriptHostRunner().ExpandTokensInArray(ro.GetCommand().GetCommand())...)
}

func (r *BuildHostServerImpl) FetchFile(fo *FetchFileOptions, s BuildHost_FetchFileServer) error {
	return SendFiles(s.Context(), r.Config.Platform.BuildPath, fo, s)
}

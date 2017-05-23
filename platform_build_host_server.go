package stonesthrow

import (
	"encoding/json"
	"golang.org/x/net/context"
)

type RepositoryPlatformGetter interface {
	GetRepository() string
	GetPlatform() string
}

type BuildHostServerImpl struct {
	Host         *HostConfig
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

func (p *BuildHostServerImpl) GetRepositoryAndPlatform(g RepositoryPlatformGetter) (*RepositoryConfig, *PlatformConfig) {
	repo_config, _ := p.Host.Repositories[g.GetRepository()]
	if repo_config == nil {
		return nil, nil
	}

	platform_config, _ := repo_config.Platforms[g.GetPlatform()]
	if platform_config == nil {
		return nil, nil
	}

	return repo_config, platform_config
}

func (p *BuildHostServerImpl) GetExecutor(s JobEventSender, platform_config *PlatformConfig) Executor {
	return NewJobEventExecutor(p.Host.Name, platform_config.BuildPath, p.ProcessAdder, s)
}

func (p *BuildHostServerImpl) GetRepositoryHostServer() RepositoryHostServer {
	return &RepositoryHostServerImpl{Host: p.Host, ProcessAdder: p.ProcessAdder}
}

func (p *BuildHostServerImpl) GetScriptHostRunner(repo *RepositoryConfig, platform *PlatformConfig) ScriptHostRunner {
	var runner ScriptHostRunner
	runner.Config.Select(p.Host, repo, platform)
	return runner
}

func (p *BuildHostServerImpl) RunScriptCommand(ro *RunOptions, s BuildHost_RunScriptCommandServer) error {
	repo, platform := p.GetRepositoryAndPlatform(ro)
	if repo == nil {
		return NewInvalidPlatformError("repository %s and platform %s are invalid", ro.GetRepository(), ro.GetPlatform())
	}
	return p.GetScriptHostRunner(repo, platform).RunScriptCommand(ro, p.GetExecutor(s, platform), s)
}

func (p *BuildHostServerImpl) ListScriptCommands(
	ctx context.Context, _ *ListCommandsOptions) (*CommandList, error) {
	repo, platform := r.GetRepositoryAndPlatform(ro)
	if repo == nil {
		return NewInvalidPlatformError("repository %s and platform %s are invalid", ro.GetRepository(), ro.GetPlatform())
	}
	return p.GetScriptHostRunner(repo, platform).ListScriptCommands(ctx, p.GetExecutor(nil, platform))
}

func (p *BuildHostServerImpl) ListTargets(context.Context, *ListTargetsOptions) (*TargetList, error) {
	return nil, NewNothingToDoError("not implemented")
}

func (p *BuildHostServerImpl) RunShellCommand(ro *RunOptions, s BuildHost_RunShellCommandServer) error {
	repo, platform := r.GetRepositoryAndPlatform(ro)
	if repo == nil {
		return NewInvalidPlatformError("repository %s and platform %s are invalid", ro.GetRepository(), ro.GetPlatform())
	}

	e := p.GetExecutor(s, platform)
	script_runner := p.GetScriptHostRunner(repo, platform)
	return e.ExecuteInWorkDirPassthrough(
		script_runner.ExpandTokens(ro.GetCommand().GetDirectory()),
		s.Context(),
		script_runner.ExpandTokensInArray(ro.GetCommand().GetCommand())...)
}

func (r *BuildHostServerImpl) FetchFile(fo *FetchFileOptions, s BuildHost_FetchFileServer) error {
	repo, platform := r.GetRepositoryAndPlatform(fo)
	if repo == nil {
		return NewInvalidPlatformError("repository %s and platform %s are invalid", fo.GetRepository(), fo.GetPlatform())
	}
	return SendFiles(s.Context(), platform.BuildPath, fo, s)
}

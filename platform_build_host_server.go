package stonesthrow

import (
	"encoding/json"
	"golang.org/x/net/context"
	"path/filepath"
	"strings"
)

type PlatformBuildHostServerImpl struct {
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

func (p *PlatformBuildHostServerImpl) GetExecutor(s JobEventSender) Executor {
	return NewJobEventExecutor(p.Config.Host.Name, p.Config.Platform.BuildPath, p.ProcessAdder, s)
}

func (p *PlatformBuildHostServerImpl) GetRepositoryHostServer() *RepositoryHostServerImpl {
	return &RepositoryHostServerImpl{Repository: p.Config.Repository, ProcessAdder: p.ProcessAdder}
}

func (p *PlatformBuildHostServerImpl) GetScript() (*Script, error) {
	if p.Script.ScriptName != "" {
		return &p.Script, nil
	}

	r := p.GetTokenReplacer()
	script_path := r.Replace(p.Config.Repository.ScriptPath)
	if script_path == "" {
		return nil, NewInvalidArgumentError("script path not defined")
	}

	p.Script = Script{
		ScriptPath:      filepath.Dir(script_path),
		ScriptName:      filepath.Base(script_path),
		StonesthrowPath: p.Config.Host.StonesthrowPath,
		Config: PlatformBuildPassthroughConfig{
			PlatformConfig: *p.Config.Platform,
			SourcePath:     p.Config.Repository.SourcePath,
			BuildPath:      p.Config.Platform.BuildPath,
			PlatformName:   p.Config.Platform.Name,
			RepositoryName: p.Config.Repository.Name,
			GomaPath:       p.Config.Host.GomaPath,
			MaxBuildJobs:   p.Config.Host.MaxBuildJobs}}

	return &p.Script, nil
}

func (p *PlatformBuildHostServerImpl) GetScriptRunner(s JobEventSender) (*ScriptRunner, error) {
	script, err := p.GetScript()
	if err != nil {
		return nil, err
	}
	script_runner := script.GetScriptRunner(p.GetExecutor(s))
	return &script_runner, nil
}

func (p *PlatformBuildHostServerImpl) Build(bo *BuildOptions, s PlatformBuildHost_BuildServer) error {
	if len(bo.GetTargets()) == 0 {
		return NewInvalidArgumentError("no targets specified")
	}

	if bo.GetPlatform() == "" {
		return NewInvalidPlatformError("platform is empty")
	}

	if p.Config.PlatformName != bo.GetPlatform() {
		return NewInvalidPlatformError("this builder only knows about %s. Requested building on %s",
			p.Config.PlatformName, bo.GetPlatform())
	}

	err := p.GetRepositoryHostServer().SyncRemote(bo.GetRepositoryState(), s)
	if err != nil {
		return err
	}

	runner, err := p.GetScriptRunner(s)
	if err != nil {
		return err
	}

	command := []string{"build"}
	command = append(command, bo.GetTargets()...)
	return runner.ExecutePassthrough(s.Context(), command...)
}

func (p *PlatformBuildHostServerImpl) GetTokenReplacer() *strings.Replacer {
	return strings.NewReplacer(
		"{src}", p.Config.Repository.SourcePath,
		"{out}", p.Config.Platform.BuildPath,
		"{st}", p.Config.Host.StonesthrowPath)
}
func (p *PlatformBuildHostServerImpl) ExpandTokens(in string) string {
	return p.GetTokenReplacer().Replace(in)
}

func (p *PlatformBuildHostServerImpl) ExpandTokensInArray(in []string) []string {
	r := p.GetTokenReplacer()
	out := []string{}
	for _, s := range in {
		out = append(out, r.Replace(s))
	}
	return out
}

func (p *PlatformBuildHostServerImpl) RunScript(ctx context.Context, ro *RunOptions, s JobEventSender) error {
	runner, err := p.GetScriptRunner(s)
	if err != nil {
		return err
	}

	return runner.ExecuteInWorkDirPassthrough(
		p.ExpandTokens(ro.GetCommand().GetDirectory()),
		ctx,
		p.ExpandTokensInArray(ro.GetCommand().GetCommand())...)
}

func (p *PlatformBuildHostServerImpl) Run(ro *RunOptions, s PlatformBuildHost_RunServer) error {
	return p.RunScript(s.Context(), ro, s)
}

func (p *PlatformBuildHostServerImpl) Clobber(co *ClobberOptions, s PlatformBuildHost_ClobberServer) error {
	command := []string{"clobber"}

	if co.GetTarget() == ClobberOptions_SOURCE {
		command = append(command, "--source")
	}

	if co.GetTarget() == ClobberOptions_OUTPUT {
		command = append(command, "--output")
	}

	if co.GetForce() {
		command = append(command, "--force")
	}

	runner, err := p.GetScriptRunner(s)
	if err != nil {
		return err
	}

	return runner.ExecutePassthrough(s.Context(), command...)
}

func (p *PlatformBuildHostServerImpl) Clean(bo *BuildOptions, s PlatformBuildHost_CleanServer) error {
	command := []string{"clean"}
	command = append(command, bo.GetTargets()...)
	runner, err := p.GetScriptRunner(s)
	if err != nil {
		return err
	}

	return runner.ExecutePassthrough(s.Context(), command...)
}

func (p *PlatformBuildHostServerImpl) Prepare(bo *BuildOptions, s PlatformBuildHost_PrepareServer) error {
	runner, err := p.GetScriptRunner(s)
	if err != nil {
		return err
	}

	return runner.ExecutePassthrough(s.Context(), "prepare")
}

func (p *PlatformBuildHostServerImpl) ListTargets(ctx context.Context, bo *BuildOptions) (*TargetList, error) {
	return nil, NewNothingToDoError("not implemented")
}

func (r *PlatformBuildHostServerImpl) FetchFile(fo *FetchFileOptions, s PlatformBuildHost_FetchFileServer) error {
	return SendFiles(s.Context(), r.Config.Platform.BuildPath, fo, s)
}

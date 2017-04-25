package stonesthrow

import (
	"encoding/json"
	"fmt"
	"golang.org/x/net/context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
			GomaPath:       p.Config.Host.GomaPath}}

	return &p.Script, nil
}

func (p *PlatformBuildHostServerImpl) IsGomaRunning(ctx context.Context, e Executor, goma_command ...string) bool {
	output, err := e.ExecuteInWorkDir(p.Config.Host.GomaPath, ctx, goma_command...)
	if err != nil {
		return false
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "compiler proxy ") &&
			strings.Contains(line, " status: ") &&
			strings.HasSuffix(line, "ok") {
			return true
		}
	}
	return false
}

func (p *PlatformBuildHostServerImpl) InvokeMbTool(ctx context.Context, e Executor, mb_command ...string) error {
	var mb_tool string
	if runtime.GOOS == "windows" {
		mb_tool = p.Config.GetSourcePath("tools", "mb", "mb.bat")
	} else {
		mb_tool = p.Config.GetSourcePath("tools", "mb", "mb.py")
	}

	command_line := append([]string{}, mb_tool, mb_command[0],
		"-c", p.Config.Platform.MbConfigName, "-g", p.Config.Host.GomaPath, p.Config.GetBuildPath())
	command_line = append(command_line, mb_command[1:]...)
	return e.ExecuteInWorkDirPassthrough(p.Config.GetSourcePath(), ctx, command_line...)
}

func (p *PlatformBuildHostServerImpl) EnsureGomaIfNecessary(ctx context.Context, e Executor) error {
	if runtime.GOOS == "windows" {
		attemptedToStartGoma := false
		for i := 0; i < 5; i += 1 {
			if p.IsGomaRunning(ctx, e, "cmd", "/c", "goma_ctl.bat", "status") {
				return nil
			}
			if !attemptedToStartGoma {
				attemptedToStartGoma = true
				gomaCommand := []string{path.Join(p.Config.Host.GomaPath, "goma_ctl.bat")}
				cmd := exec.CommandContext(ctx, "cmd.exe", "/c", gomaCommand[0], "ensure_start")
				err := cmd.Start()
				// Don't wait for 'goma_ctl.bat ensure_start' to terminate. It won't.
				if err != nil {
					return err
				}
			}
			time.Sleep(time.Second)
		}
		return NewTimedOutError("Couldn't start compiler proxy.")
	} else {
		if p.IsGomaRunning(ctx, e, "python", "goma_ctl.py", "status") {
			return nil
		}
		return e.ExecuteInWorkDirPassthrough(p.Config.Host.GomaPath, ctx, "python", "goma_ctl.py", "ensure_start")
	}
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

	e := p.GetExecutor(s)
	err := p.EnsureGomaIfNecessary(s.Context(), e)
	if err != nil {
		return err
	}

	err = p.GetRepositoryHostServer().SyncRemote(bo.GetRepositoryState(), s)
	if err != nil {
		return err
	}

	command := []string{"ninja"}
	if p.Config.Host.MaxBuildJobs != 0 {
		command = append(command, "-j", fmt.Sprintf("%d", p.Config.Host.MaxBuildJobs))
	}
	command = append(command, bo.GetTargets()...)
	return e.ExecutePassthrough(s.Context(), command...)
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

func (p *PlatformBuildHostServerImpl) RunScript(ro *RunOptions, s PlatformBuildHost_RunServer) error {
	e := p.GetExecutor(s)
	script, err := p.GetScript()
	if err != nil {
		return err
	}

	runner := script.GetScriptRunner(e)

	return runner.ExecuteInWorkDirPassthrough(
		p.ExpandTokens(ro.GetCommand().GetDirectory()),
		s.Context(),
		p.ExpandTokensInArray(ro.GetCommand().GetCommand())...)
}

func (p *PlatformBuildHostServerImpl) Run(ro *RunOptions, s PlatformBuildHost_RunServer) error {
	return p.RunScript(ro, s)
}

func (p *PlatformBuildHostServerImpl) GetDependenciesFromCommand(command []string, dir string) []string {
	if len(command) == 0 {
		return nil
	}

	var command_path string

	if filepath.IsAbs(command[0]) {
		command_path = command[0]
	} else if strings.ContainsAny(command[0], "/\\") {
		command_path = filepath.Join(dir, command[0])
	} else {
		// Otherwise the executable is getting picked up from PATH and
		// is unlikely to be a build artifact.
		return nil
	}

	if filepath.Dir(command_path) != p.Config.GetBuildPath() {
		return nil
	}

	return []string{filepath.Base(command_path)}
}

func (p *PlatformBuildHostServerImpl) RunCommand(ro *RunOptions, s PlatformBuildHost_RunServer) error {
	if len(ro.GetCommand().GetCommand()) == 0 {
		return NewNothingToDoError("no commands specified")
	}

	command := p.ExpandTokensInArray(ro.GetCommand().GetCommand())
	dir := p.ExpandTokens(ro.GetCommand().GetDirectory())

	build_targets := ro.GetDependencies().GetTarget()

	if ro.GetAutomaticDependencies() {
		build_targets = append(build_targets, p.GetDependenciesFromCommand(command, dir)...)
	}

	if len(build_targets) > 0 {
		bo := BuildOptions{
			Platform:        ro.GetPlatform(),
			Targets:         build_targets,
			RepositoryState: ro.GetRepositoryState()}
		err := p.Build(&bo, s)
		if err != nil {
			return err
		}
	}

	e := p.GetExecutor(s)
	return e.ExecuteInWorkDirPassthrough(dir, s.Context(), command...)
}

func (p *PlatformBuildHostServerImpl) Clobber(co *ClobberOptions, s PlatformBuildHost_ClobberServer) error {
	if co.GetPlatform() == "" {
		return NewInvalidPlatformError("platform is empty")
	}

	if co.GetTarget() == ClobberOptions_SOURCE {
		repo := p.GetRepositoryHostServer()
		_, commands := repo.GetGitCommandsForJobEventSender(s)
		command := []string{"git", "clean"}
		if co.GetForce() {
			command = append(command, "--force")
		}
		return commands.ExecutePassthrough(s.Context(), command...)
	}

	if !co.GetForce() {
		s.Send(&JobEvent{
			LogEvent: &LogEvent{
				Host:     p.Config.Host.Name,
				Severity: LogEvent_INFO,
				Msg:      fmt.Sprintf("Will remove %s", p.Config.Platform.BuildPath)}})
		return nil
	}

	err := os.RemoveAll(p.Config.Platform.BuildPath)
	if err != nil {
		return err
	}

	return p.Prepare(nil, s)
}

func (p *PlatformBuildHostServerImpl) Clean(bo *BuildOptions, s PlatformBuildHost_CleanServer) error {
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

	command := []string{"ninja"}
	if p.Config.Host.MaxBuildJobs != 0 {
		command = append(command, "-j", fmt.Sprintf("%d", p.Config.Host.MaxBuildJobs))
	}
	command = append(command, "-t", "clean")
	command = append(command, bo.GetTargets()...)

	e := p.GetExecutor(s)
	return e.ExecutePassthrough(s.Context(), command...)
}

func (p *PlatformBuildHostServerImpl) Prepare(bo *BuildOptions, s PlatformBuildHost_PrepareServer) error {
	e := p.GetExecutor(s)
	return p.InvokeMbTool(s.Context(), e, "gen")
}

func (p *PlatformBuildHostServerImpl) ListTargets(ctx context.Context, bo *BuildOptions) (*TargetList, error) {
	return nil, NewNothingToDoError("not implemented")
}

func (r *PlatformBuildHostServerImpl) FetchFile(fo *FetchFileOptions, s PlatformBuildHost_FetchFileServer) error {
	return SendFiles(s.Context(), r.Config.Platform.BuildPath, fo, s)
}

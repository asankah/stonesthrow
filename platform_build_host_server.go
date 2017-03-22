package stonesthrow

import (
	"fmt"
	"golang.org/x/net/context"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"
)

type PlatformBuildHostServerImpl struct {
	Config       Config
	ProcessAdder ProcessAdder
}

func (p *PlatformBuildHostServerImpl) GetExecutor(s JobEventSender) Executor {
	return NewJobEventExecutor(p.Config.Host.Name, p.Config.Platform.BuildPath, p.ProcessAdder, s)
}

func (p *PlatformBuildHostServerImpl) GetRepositoryHostServer() *RepositoryHostServerImpl {
	return &RepositoryHostServerImpl{Repository: p.Config.Repository, ProcessAdder: p.ProcessAdder}
}

func (p *PlatformBuildHostServerImpl) EnsureGomaIfNecessary(ctx context.Context, e Executor) error {
	gomaCtlStampFile := p.Config.Platform.RelativePath("goma_ensure_start_stamp")
	fileInfo, ok := os.Stat(gomaCtlStampFile)
	if ok == nil && fileInfo.ModTime().Add(time.Second*60*60*6).After(time.Now()) {
		return nil
	}

	if stampFile, ok := os.Create(gomaCtlStampFile); ok == nil {
		stampFile.Close()
	}

	if runtime.GOOS == "windows" {
		attemptedToStartGoma := false
		for i := 0; i < 5; i += 1 {
			output, err := e.ExecuteInWorkDirNoStream(p.Config.Host.GomaPath, ctx, "cmd", "/c", "goma_ctl.bat", "status")
			if err != nil {
				return err
			}
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "compiler proxy ") &&
					strings.Contains(line, " status: ") &&
					strings.HasSuffix(line, "ok") {
					return nil
				}
			}

			if !attemptedToStartGoma {
				attemptedToStartGoma = true
				gomaCommand := []string{path.Join(p.Config.Host.GomaPath, "goma_ctl.bat")}
				cmd := exec.CommandContext(ctx, "cmd.exe", "/c", gomaCommand[0], "ensure_start")
				err = cmd.Start()
				// Don't wait for 'goma_ctl.bat ensure_start' to terminate. It won't.
				if err != nil {
					return err
				}
			}
			time.Sleep(time.Second)
		}
		return NewTimedOutError("Couldn't start compiler proxy.")
	} else {
		return e.ExecuteInWorkDirPassthrough(p.Config.Host.GomaPath, ctx, "goma_ctl.py", "ensure_start")
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

	for _, target := range bo.GetTargets() {
		if strings.HasSuffix(target, "_run") {
			isolated_filename := p.Config.Platform.RelativePath(target[:len(target)-4] + ".isolated")
			os.Remove(isolated_filename)
		}
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

func (p *PlatformBuildHostServerImpl) Run(ro *RunOptions, s PlatformBuildHost_RunServer) error {
	return NewNothingToDoError("not implemented")
}

func (p *PlatformBuildHostServerImpl) Clobber(co *ClobberOptions, s PlatformBuildHost_ClobberServer) error {
	return NewNothingToDoError("not implemented")
}

func (p *PlatformBuildHostServerImpl) Clean(bo *BuildOptions, s PlatformBuildHost_CleanServer) error {
	return NewNothingToDoError("not implemented")
}

func (p *PlatformBuildHostServerImpl) Prepare(bo *BuildOptions, s PlatformBuildHost_PrepareServer) error {
	return NewNothingToDoError("not implemented")
}

func (p *PlatformBuildHostServerImpl) ListTargets(ctx context.Context, bo *BuildOptions) (*TargetList, error) {
	return nil, NewNothingToDoError("not implemented")
}

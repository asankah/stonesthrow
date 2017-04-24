package stonesthrow

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
)

type PassthroughConfig interface {
	AsJson() string
}

type Script struct {
	ScriptPath      string
	ScriptName      string
	StonesthrowPath string
	Config          PassthroughConfig
}

func (s Script) GetScriptRunnerCommand() []string {
	return []string{
		"python", filepath.Join(s.StonesthrowPath, "python", "stonesthrow", "host.py"),
		"--sys_path", s.StonesthrowPath,
		"--sys_path", s.ScriptPath,
		"--module", s.ScriptName,
		"--config", s.Config.AsJson()}
}

type ScriptRunner struct {
	Script
	Executor Executor
}

func (s ScriptRunner) ExecutePassthrough(ctx context.Context, args ...string) error {
	if len(args) == 0 {
		return NewInvalidArgumentError("Need script name")
	}

	if _, err := os.Stat(s.ScriptPath); os.IsNotExist(err) {
		return err
	}

	return Executor.ExecutePassthrough(ctx, append(s.GetScriptRunnerCommand(), args)...)
}

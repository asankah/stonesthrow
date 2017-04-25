package stonesthrow

import (
	"context"
	"os"
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

func (s Script) GetScriptRunner(e Executor) ScriptRunner {
	return ScriptRunner{
		Script{
			ScriptPath:      s.ScriptPath,
			ScriptName:      s.ScriptName,
			StonesthrowPath: s.StonesthrowPath,
			Config:          s.Config}, e}
}

func (s ScriptRunner) ExecutePassthrough(ctx context.Context, args ...string) error {
	return s.ExecuteInWorkDirPassthrough(s.ScriptPath, ctx, args...)
}

func (s ScriptRunner) ExecuteInWorkDirPassthrough(work_dir string, ctx context.Context, args ...string) error {
	if s.ScriptPath == "" || s.ScriptName == "" || s.Executor == nil || s.StonesthrowPath == "" {
		return NewConfigIncompleteError("script configuration incomplete")
	}
	if _, err := os.Stat(s.ScriptPath); os.IsNotExist(err) {
		return err
	}

	return s.Executor.ExecuteInWorkDirPassthrough(work_dir, ctx, append(s.GetScriptRunnerCommand(), args...)...)
}

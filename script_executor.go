package stonesthrow

import (
	"context"
	"encoding/json"
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

func (s Script) GetScriptRunnerCommand(args ...string) []string {
	return append([]string{
		"python", filepath.Join(s.StonesthrowPath, "python", "stonesthrow", "host.py"),
		"--sys_path", s.StonesthrowPath,
		"--sys_path", s.ScriptPath,
		"--module", s.ScriptName,
		"--config", s.Config.AsJson()}, args...)
}

type ScriptExecutor struct {
	Script
	Executor Executor
}

func (s Script) GetScriptRunner(e Executor) ScriptExecutor {
	return ScriptExecutor{
		Script{
			ScriptPath:      s.ScriptPath,
			ScriptName:      s.ScriptName,
			StonesthrowPath: s.StonesthrowPath,
			Config:          s.Config}, e}
}

func (s ScriptExecutor) Validate() error {
	if s.ScriptPath == "" || s.ScriptName == "" || s.Executor == nil || s.StonesthrowPath == "" {
		return NewConfigIncompleteError("script configuration incomplete")
	}
	if _, err := os.Stat(s.ScriptPath); os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s ScriptExecutor) NeedsSource(ctx context.Context, args ...string) (bool, error) {
	type BoolContainer struct {
		Result bool `json:"result"`
	}

	output, err := s.ExecuteNoStream(ctx, append([]string{"--verify-source-needed"}, args...)...)
	if err != nil {
		return false, err
	}

	var bool_container BoolContainer
	err = json.Unmarshal([]byte(output), &bool_container)
	if err != nil {
		return false, err
	}
	return bool_container.Result, nil
}

func (s ScriptExecutor) ListCommands(ctx context.Context) (*CommandList, error) {
	output, err := s.ExecuteNoStream(ctx, "--list-commands")
	if err != nil {
		return nil, err
	}

	var command_list CommandList
	err = json.Unmarshal([]byte(output), &command_list)
	if err != nil {
		return nil, err
	}

	return &command_list, nil
}

func (s ScriptExecutor) OnRepositoryCheckout(ctx context.Context) error {
	return s.ExecutePassthrough(ctx, "--event", "checkout")
}

func (s ScriptExecutor) ExecutePassthrough(ctx context.Context, args ...string) error {
	return s.ExecuteInWorkDirPassthrough(s.ScriptPath, ctx, args...)
}

func (s ScriptExecutor) ExecuteInWorkDirPassthrough(work_dir string, ctx context.Context, args ...string) error {
	err := s.Validate()
	if err != nil {
		return err
	}
	return s.Executor.ExecuteInWorkDirPassthrough(work_dir, ctx, s.GetScriptRunnerCommand(args...)...)
}

func (s ScriptExecutor) Execute(ctx context.Context, args ...string) (string, error) {
	return s.ExecuteInWorkDir(s.ScriptPath, ctx, args...)
}

func (s ScriptExecutor) ExecuteInWorkDir(work_dir string, ctx context.Context, args ...string) (string, error) {
	err := s.Validate()
	if err != nil {
		return "", err
	}
	return s.Executor.ExecuteInWorkDir(work_dir, ctx, s.GetScriptRunnerCommand(args...)...)
}

func (s ScriptExecutor) ExecuteNoStream(ctx context.Context, args ...string) (string, error) {
	return s.ExecuteInWorkDirNoStream(s.ScriptPath, ctx, args...)
}

func (s ScriptExecutor) ExecuteInWorkDirNoStream(work_dir string, ctx context.Context, args ...string) (string, error) {
	err := s.Validate()
	if err != nil {
		return "", err
	}
	return s.Executor.ExecuteInWorkDirNoStream(work_dir, ctx, s.GetScriptRunnerCommand(args...)...)
}

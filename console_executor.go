package stonesthrow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type ConsoleExecutor struct {
	HideStdout bool
	HideStderr bool
}

const (
	MaxReadBufferSize = 1024 * 1024 * 1024
)

func (c ConsoleExecutor) ExecuteSilently(ctx context.Context, workdir string, command ...string) (string, error) {
	return RunCommandWithWorkDir(ctx, workdir, command...)
}

func (c ConsoleExecutor) Execute(ctx context.Context, workdir string, command ...string) error {
	if len(command) == 0 {
		return NewEmptyCommandError("")
	}
	fmt.Printf("Running %s\n", strings.Join(command, " "))
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workdir
	cmd.Env = nil
	cmd.Stdin = nil
	if !c.HideStderr {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = nil
	}
	if !c.HideStdout {
		cmd.Stdout = os.Stdout
	} else {
		cmd.Stdout = nil
	}
	return cmd.Run()
}

func (c ConsoleExecutor) ExecuteWithOutput(ctx context.Context,
	workdir string, command ...string) (string, error) {
	if len(command) == 0 {
		return "", NewEmptyCommandError("")
	}
	fmt.Printf("Running %s\n", strings.Join(command, " "))
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workdir
	cmd.Env = nil
	cmd.Stdin = nil

	var output bytes.Buffer
	cmd.Stdout = &output

	if !c.HideStderr {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = nil
	}

	err := cmd.Run()
	os.Stdout.Write(output.Bytes())

	return strings.TrimSpace(output.String()), err
}

func RunCommandWithWorkDir(ctx context.Context, workdir string, command ...string) (string, error) {
	if len(command) == 0 {
		return "", NewEmptyCommandError("")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workdir
	cmd.Env = nil
	output, err := cmd.Output()
	if err != nil {
		return "", errors.New(fmt.Sprintf("Failed to execute {%s}: %s", command, err.Error()))
	}
	return strings.TrimSpace(string(output)), nil
}

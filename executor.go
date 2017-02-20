package stonesthrow

import (
	"os/exec"
	"os"
	"errors"
	"strings"
)

type Executor interface {
	RunCommand(workdir string, command ...string) (string, error)
	CheckCommand(workdir string, command ...string) error
}

type ConsoleExecutor struct {
}

func (c ConsoleExecutor) RunCommand(workdir string, command ...string) (string, error) {
	return RunCommandWithWorkDir(workdir, command...)
}

func (c ConsoleExecutor) CheckCommand(workdir string, command ...string) error {
	if len(command) == 0 {
		return "", EmptyCommandError
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = workdir
	cmd.Env = nil
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func RunCommandWithWorkDir(workdir string, command ...string) (string, error) {
	if len(command) == 0 {
		return "", EmptyCommandError
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = workdir
	cmd.Env = nil
	output, err := cmd.Output()
	if err != nil {
		return "", errors.New(fmt.Sprintf("Failed to execute {%s}: %s", command, err.Error()))
	}
	return strings.TrimSpace(string(output)), nil
}


package stonesthrow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type ConsoleExecutor struct {
	processAdder ProcessAdder
	channel      Channel
	label        string
}

const (
	MaxReadBufferSize = 1024 * 1024 * 1024
)

func NewConsoleExecutorForMessageHandler(handler chan interface{}, label string) ConsoleExecutor {
	return ConsoleExecutor{
		channel:      Channel{conn: LocalStaticConnection{ResponseSink: handler}},
		processAdder: nil,
		label:        label}
}

func (c ConsoleExecutor) ExecuteSilently(ctx context.Context, workdir string, command ...string) (string, error) {
	return RunCommandWithWorkDir(ctx, workdir, command...)
}

func (c ConsoleExecutor) execute(ctx context.Context, workdir string, captureStdout bool, command ...string) (string, error) {
	// Nothing to do?
	if len(command) == 0 {
		return "", NewEmptyCommandError("")
	}

	c.channel.BeginCommand(c.label, workdir, command, false)
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Env = nil // inherit
	cmd.Dir = workdir

	quitter := make(chan int)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		c.channel.Error(fmt.Sprintf("Can't open stderr pipe: %s", err.Error()))
		return "", err
	}
	go func() {
		c.channel.Stream(stderrPipe)
		quitter <- 2
	}()

	var output bytes.Buffer
	var stdoutPipe io.ReadCloser
	if captureStdout {
		cmd.Stdout = &output
	} else {
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			c.channel.Error(fmt.Sprintf("Can't open stdout pipe: %s", err.Error()))
			return "", err
		}
		go func() {
			c.channel.Stream(stdoutPipe)
			quitter <- 1
		}()
	}

	cmd.Start()
	if c.processAdder != nil {
		c.processAdder.AddProcess(command, cmd.Process)
	}
	err = cmd.Wait()

	stderrPipe.Close()
	<-quitter

	var outputString string
	if captureStdout {
		c.channel.Stream(bytes.NewReader(output.Bytes()))
		outputString = strings.TrimSpace(output.String())
	} else {
		stdoutPipe.Close()
		<-quitter
	}

	if err != nil {
		return outputString, err
	}
	if c.processAdder != nil {
		c.processAdder.RemoveProcess(cmd.Process, cmd.ProcessState)
	}
	c.channel.EndCommand(cmd.ProcessState)
	if cmd.ProcessState.Success() {
		return outputString, nil
	}

	return outputString, NewExternalCommandFailedError("")
}

func (c ConsoleExecutor) Execute(ctx context.Context, workdir string, command ...string) error {
	_, err := c.execute(ctx, workdir, false, command...)
	return err
}

func (c ConsoleExecutor) ExecuteWithOutput(ctx context.Context, workdir string, command ...string) (string, error) {
	return c.execute(ctx, workdir, true, command...)
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

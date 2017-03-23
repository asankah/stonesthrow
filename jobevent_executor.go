package stonesthrow

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type JobEventExecutor struct {
	host         string
	workdir      string
	processAdder ProcessAdder
	sender       JobEventSender
}

func NewJobEventExecutor(
	host string,
	workdir string,
	processAdder ProcessAdder,
	sender JobEventSender) *JobEventExecutor {
	if sender == nil {
		sender = NilJobEventSender{}
	}
	return &JobEventExecutor{
		host:         host,
		workdir:      workdir,
		processAdder: processAdder,
		sender:       sender}
}

func (e JobEventExecutor) stream(stream CommandOutputEvent_Stream, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		e.sender.Send(&JobEvent{
			CommandOutputEvent: &CommandOutputEvent{
				Stream: stream,
				Output: scanner.Text()}})
	}
}

func (e JobEventExecutor) execute(ctx context.Context, workdir string, captureStdout bool, command ...string) (string, error) {
	// Nothing to do?
	if len(command) == 0 {
		return "", NewEmptyCommandError("")
	}

	err := e.sender.Send(&JobEvent{
		BeginCommandEvent: &BeginCommandEvent{
			Command: &ShellCommand{
				Command:   command,
				Directory: workdir,
				Host:      e.host}}})
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Env = nil // inherit
	cmd.Dir = workdir

	quitter := make(chan int)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		e.sender.Send(&JobEvent{
			LogEvent: &LogEvent{
				Host:     e.host,
				Msg:      fmt.Sprintf("Can't open stderr pipe: %s", err.Error()),
				Severity: LogEvent_ERROR}})
		return "", err
	}
	go func() {
		e.stream(CommandOutputEvent_ERR, stderrPipe)
		quitter <- 2
	}()

	var output bytes.Buffer
	var stdoutPipe io.ReadCloser
	if captureStdout {
		cmd.Stdout = &output
	} else {
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			e.sender.Send(&JobEvent{
				LogEvent: &LogEvent{
					Host:     e.host,
					Msg:      fmt.Sprintf("Can't open stdout pipe: %s", err.Error()),
					Severity: LogEvent_ERROR}})
			return "", err
		}
		go func() {
			e.stream(CommandOutputEvent_OUT, stdoutPipe)
			quitter <- 1
		}()
	}

	cmd.Start()
	if e.processAdder != nil {
		e.processAdder.AddProcess(command, cmd.Process)
	}
	err = cmd.Wait()

	stderrPipe.Close()
	<-quitter

	var outputString string
	if captureStdout {
		e.stream(CommandOutputEvent_OUT, bytes.NewReader(output.Bytes()))
		outputString = strings.TrimSpace(output.String())
	} else {
		stdoutPipe.Close()
		<-quitter
	}

	if err != nil {
		return outputString, err
	}
	if e.processAdder != nil {
		e.processAdder.RemoveProcess(cmd.Process, cmd.ProcessState)
	}
	var fake_return_code int32
	if !cmd.ProcessState.Success() {
		fake_return_code = 1
	}
	e.sender.Send(&JobEvent{
		EndCommandEvent: &EndCommandEvent{
			ReturnCode: fake_return_code,
			SystemTime: NewDurationFromDuration(cmd.ProcessState.SystemTime()),
			UserTime:   NewDurationFromDuration(cmd.ProcessState.UserTime())}})

	if cmd.ProcessState.Success() {
		return outputString, nil
	}

	return outputString, NewExternalCommandFailedError("")
}

func (e JobEventExecutor) ExecuteInWorkDirNoStream(workdir string, ctx context.Context, command ...string) (string, error) {
	return RunCommandWithWorkDir(ctx, workdir, command...)
}

func (e JobEventExecutor) ExecuteInWorkDir(workdir string, ctx context.Context, command ...string) (string, error) {
	return e.execute(ctx, workdir, true, command...)
}

func (e JobEventExecutor) ExecuteInWorkDirPassthrough(workdir string, ctx context.Context, command ...string) error {
	_, err := e.execute(ctx, workdir, false, command...)
	return err
}

func (e JobEventExecutor) ExecutePassthrough(ctx context.Context, command ...string) error {
	return e.ExecuteInWorkDirPassthrough(e.workdir, ctx, command...)
}

func (e JobEventExecutor) ExecuteNoStream(ctx context.Context, command ...string) (string, error) {
	return e.ExecuteInWorkDirNoStream(e.workdir, ctx, command...)
}

func (e JobEventExecutor) Execute(ctx context.Context, command ...string) (string, error) {
	return e.ExecuteInWorkDir(e.workdir, ctx, command...)
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
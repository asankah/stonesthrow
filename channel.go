package stonesthrow

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

type Channel struct {
	conn Connection
}

func (c Channel) Info(message string) {
	c.conn.Send(InfoMessage{Info: message})
}

func (c Channel) Error(message string) {
	c.conn.Send(ErrorMessage{Error: message})
}

func (c Channel) Stream(stream io.Reader) {
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		c.conn.Send(TerminalOutputMessage{Output: scanner.Text()})
	}
}

func (c Channel) BeginCommand(hostname string, workdir string, command []string, isInteractive bool) {
	c.conn.Send(BeginCommandMessage{
		IsInteractive: isInteractive,
		WorkDir:       workdir,
		Hostname:      hostname,
		Command:       command})
}

func (c Channel) EndCommand(processState *os.ProcessState) {
	returnCode := 0
	if !processState.Success() {
		returnCode = 1
	}
	c.conn.Send(EndCommandMessage{
		ReturnCode:   returnCode,
		SystemTimeNs: processState.SystemTime().Nanoseconds(),
		UserTimeNs:   processState.UserTime().Nanoseconds()})
}

func (c Channel) ListCommands(commandList CommandListMessage) {
	c.conn.Send(commandList)
}

func (c Channel) ListProcesses(processList ProcessListMessage) {
	c.conn.Send(processList)
}

func (c Channel) Send(message interface{}) error {
	return c.conn.Send(message)
}

func (c Channel) Receive() (interface{}, error) {
	r, e := c.conn.Receive()
	return r, e
}

func (c Channel) NewSendChannel() chan interface{} {
	channel := make(chan interface{})
	go func() {
		for message := range channel {
			switch t := message.(type) {
			case *TerminalOutputMessage:
				c.Send(*t)

			case *InfoMessage:
				c.Send(*t)

			case *ErrorMessage:
				c.Send(*t)

			case *BeginCommandMessage:
				c.Send(*t)

			case *EndCommandMessage:
				c.Send(*t)

			case *CommandListMessage:
				c.Send(*t)

			case *JobListMessage:
				c.Send(*t)

			case *RequestMessage:
				c.Send(*t)
			}
		}
	}()

	return channel
}

func (c Channel) ListJobs(joblist JobListMessage) {
	c.conn.Send(joblist)
}

func (c Channel) Request(req RequestMessage) {
	c.conn.Send(req)
}

func (c Channel) SwapConnection(nc Connection) Connection {
	oldConnection := c.conn
	c.conn = nc
	return oldConnection
}

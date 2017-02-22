package stonesthrow

import (
	"bufio"
	"io"
	"os"
	"sync"
)

type Channel struct {
	conn Connection
	mut  sync.Mutex
}

func (c Channel) Info(message string) {
	c.mut.Lock()
	c.conn.Send(InfoMessage{Info: message})
	c.mut.Unlock()
}

func (c Channel) Error(message string) {
	c.mut.Lock()
	c.conn.Send(ErrorMessage{Error: message})
	c.mut.Unlock()
}

func (c Channel) Stream(stream io.Reader) {
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		c.mut.Lock()
		c.conn.Send(TerminalOutputMessage{Output: scanner.Text()})
		c.mut.Unlock()
	}
}

func (c Channel) BeginCommand(command []string, isInteractive bool) {
	c.mut.Lock()
	c.conn.Send(BeginCommandMessage{IsInteractive: isInteractive,
		Command: command})
	c.mut.Unlock()
}

func (c Channel) EndCommand(processState *os.ProcessState) {
	returnCode := 0
	if !processState.Success() {
		returnCode = 1
	}
	c.mut.Lock()
	c.conn.Send(EndCommandMessage{
		ReturnCode:   returnCode,
		SystemTimeNs: processState.SystemTime().Nanoseconds(),
		UserTimeNs:   processState.UserTime().Nanoseconds()})
	c.mut.Unlock()
}

func (c Channel) ListCommands(commandList CommandListMessage) {
	c.mut.Lock()
	c.conn.Send(commandList)
	c.mut.Unlock()
}

func (c Channel) ListProcesses(processList ProcessListMessage) {
	c.mut.Lock()
	c.conn.Send(processList)
	c.mut.Unlock()
}

func (c Channel) Receive() (interface{}, error) {
	c.mut.Lock()
	r, e := c.conn.Receive()
	c.mut.Unlock()
	return r, e
}

func (c Channel) ListJobs(joblist JobListMessage) {
	c.mut.Lock()
	c.conn.Send(joblist)
	c.mut.Unlock()
}

func (c Channel) SwapConnection(nc Connection) Connection {
	c.mut.Lock()
	defer c.mut.Unlock()
	oldConnection := c.conn
	c.conn = nc
	return oldConnection
}

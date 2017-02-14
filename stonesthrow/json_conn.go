package stonesthrow

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type jsonConnection struct {
	stream *bufio.ReadWriter
	encoder *gob.Encoder
	decoder *gob.Decoder
}

type jsonWrapper struct {
	Output       *TerminalOutputMessage `json:"output,omitempty"`
	Info         *InfoMessage           `json:"info,omitempty"`
	Error        *ErrorMessage          `json:"error,omitempty"`
	BeginCommand *BeginCommandMessage   `json:"begin,omitempty"`
	EndCommand   *EndCommandMessage     `json:"end,omitempty"`
	CommandList  *CommandListMessage    `json:"ls,omitempty"`
	Request      *RequestMessage        `json:"req,omitempty"`
	JobList      *JobListMessage        `json:"jobs,omitempty"`
}

func (c jsonConnection) Receive() (interface{}, error) {
	if c.decoder == nil {
		c.decoder = gob.NewDecoder(c.stream)
	}

	var wrapper jsonWrapper
	err := c.decoder.Decode(&wrapper)
	if err == io.EOF {
		return nil, err
	}
	if err != nil {
		log.Printf("Can't decode stream: %#v", err)
		return nil, err
	}

	switch {
	case wrapper.Output != nil:
		return wrapper.Output, nil

	case wrapper.BeginCommand != nil:
		return wrapper.BeginCommand, nil

	case wrapper.EndCommand != nil:
		return wrapper.EndCommand, nil

	case wrapper.Info != nil:
		return wrapper.Info, nil

	case wrapper.Error != nil:
		return wrapper.Error, nil

	case wrapper.CommandList != nil:
		return wrapper.CommandList, nil

	case wrapper.JobList != nil:
		return wrapper.JobList, nil

	case wrapper.Request != nil:
		return wrapper.Request, nil
	}
	return nil, InvalidMessageType
}

func (c jsonConnection) Send(message interface{}) error {
	var wrapper jsonWrapper
	if c.encoder == nil {
		c.encoder = gob.NewEncoder(c.stream)
	}

	switch t := message.(type) {
	case TerminalOutputMessage:
		wrapper.Output = &t

	case InfoMessage:
		wrapper.Info = &t

	case ErrorMessage:
		wrapper.Error = &t

	case BeginCommandMessage:
		wrapper.BeginCommand = &t

	case EndCommandMessage:
		wrapper.EndCommand = &t

	case CommandListMessage:
		wrapper.CommandList = &t

	case JobListMessage:
		wrapper.JobList = &t

	case RequestMessage:
		wrapper.Request = &t

	default:
		log.Fatalf("Unexpected message type")
	}

	err := c.encoder.Encode(wrapper)
	if err == nil {
		c.stream.Flush()
	}
	return err
}

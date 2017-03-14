package stonesthrow

import (
	"io"
)

type LocalStaticConnection struct {
	ResponseSink chan interface{}
}

func (l LocalStaticConnection) Receive() (interface{}, error) {
	return nil, io.EOF
}

func (l LocalStaticConnection) Send(message interface{}) error {
	switch t := message.(type) {
	case TerminalOutputMessage:
		l.ResponseSink <- &t

	case InfoMessage:
		l.ResponseSink <- &t

	case ErrorMessage:
		l.ResponseSink <- &t

	case BeginCommandMessage:
		l.ResponseSink <- &t

	case EndCommandMessage:
		l.ResponseSink <- &t

	case CommandListMessage:
		l.ResponseSink <- &t

	case JobListMessage:
		l.ResponseSink <- &t

	case RequestMessage:
		l.ResponseSink <- &t
	}
	return nil
}

func (l LocalStaticConnection) Close() {}

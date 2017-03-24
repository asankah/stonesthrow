package stonesthrow

import "fmt"

type JobEventSender interface {
	Send(*JobEvent) error
}

type NilJobEventSender struct{}

func (_ NilJobEventSender) Send(*JobEvent) error {
	return nil
}

type ConsoleJobEventSender struct{}

func (_ ConsoleJobEventSender) Send(e *JobEvent) error {
	fmt.Printf("%#v\n", e)
	return nil
}

type TimestampingJobEventSender struct {
	sender JobEventSender
}

func (t TimestampingJobEventSender) Send(e *JobEvent) error {
	if e.Time == nil {
		e.Time = TimestampNow()
	}
	return t.sender.Send(e)
}

func DrainJobEventPipe(receiver JobEventReceiver, sender JobEventSender) error {
	for {
		je, err := receiver.Recv()
		if err != nil {
			return err
		}

		err = sender.Send(je)
		if err != nil {
			return err
		}
	}
}

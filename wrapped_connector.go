package stonesthrow

import (
	"encoding/gob"
	"io"
	"log"
	"runtime/debug"
	"sync"
)

type WrappedMessageConnector struct {
	in  io.Reader
	out io.Writer

	inChannel  chan WrappedMessage
	outChannel chan WrappedMessage
	once       sync.Once
}

func reader(in io.Reader, c chan WrappedMessage) {
	decoder := gob.NewDecoder(in)
	defer close(c)
	for {
		var wrapper WrappedMessage
		err := decoder.Decode(&wrapper)

		if err == io.EOF {
			return
		}

		if err != nil {
			log.Printf("Can't decode stream: %#v: %s", err, err.Error())
			debug.PrintStack()
			return
		}
		c <- wrapper
	}
}

func writer(out io.Writer, c chan WrappedMessage) {
	encoder := gob.NewEncoder(out)
	for message := range c {
		encoder.Encode(message)
	}
}

func (c *WrappedMessageConnector) Init() {
	pc := c
	c.once.Do(func() {
		pc.inChannel = make(chan WrappedMessage)
		pc.outChannel = make(chan WrappedMessage)
		go reader(pc.in, pc.inChannel)
		go writer(pc.out, pc.outChannel)
	})
}

func (c WrappedMessageConnector) Receive() (interface{}, error) {
	// Note that if the |c.inChannel| is closed, then the channel returns
	// the zero value for WrappedMessage.
	return UnwrapMessage(<-c.inChannel)
}

func (c WrappedMessageConnector) Send(message interface{}) error {
	wrapper, err := WrapMessage(message)
	if err != nil {
		return err
	}
	c.outChannel <- wrapper
	return nil
}

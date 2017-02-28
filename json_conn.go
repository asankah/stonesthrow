package stonesthrow

import (
	"encoding/json"
	"io"
	"log"
	"sync"
)

type jsonConnection struct {
	in  io.Reader
	out io.Writer

	inChannel  chan WrappedMessage
	outChannel chan WrappedMessage
	once       sync.Once
}

func reader(in io.Reader, c chan WrappedMessage) {
	decoder := json.NewDecoder(in)
	defer close(c)
	for {
		var wrapper WrappedMessage
		err := decoder.Decode(&wrapper)

		if err == io.EOF {
			return
		}

		if err != nil {
			log.Printf("Can't decode stream: %#v", err)
			return
		}
		c <- wrapper
	}
}

func writer(out io.Writer, c chan WrappedMessage) {
	encoder := json.NewEncoder(out)
	for message := range c {
		encoder.Encode(message)
	}
}

func (c *jsonConnection) Init() {
	pc := c
	c.once.Do(func() {
		pc.inChannel = make(chan WrappedMessage)
		pc.outChannel = make(chan WrappedMessage)
		go reader(pc.in, pc.inChannel)
		go writer(pc.out, pc.outChannel)
	})
}

func (c jsonConnection) Receive() (interface{}, error) {
	return UnwrapMessage(<-c.inChannel)
}

func (c jsonConnection) Send(message interface{}) error {
	wrapper, err := WrapMessage(message)
	if err != nil {
		return err
	}
	c.outChannel <- wrapper
	return nil
}

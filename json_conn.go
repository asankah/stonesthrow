package stonesthrow

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
	"sync"
)

type jsonConnection struct {
	stream *bufio.ReadWriter
	mut sync.Mutex
	encoder *gob.Encoder
	decoder *gob.Decoder
}

func (c jsonConnection) Receive() (interface{}, error) {
	var wrapper WrappedMessage

	c.mut.Lock()
	if c.decoder == nil {
		c.decoder = gob.NewDecoder(c.stream)
	}
	err := c.decoder.Decode(&wrapper)
	c.mut.Unlock()

	if err == io.EOF {
		return nil, err
	}

	if err != nil {
		log.Printf("Can't decode stream: %#v", err)
		return nil, err
	}
	return UnwrapMessage(wrapper)
}

func (c jsonConnection) Send(message interface{}) error {
	c.mut.Lock()
	if c.encoder == nil {
		c.encoder = gob.NewEncoder(c.stream)
	}
	err := c.encoder.Encode(WrapMessage(message))
	if err == nil {
		c.stream.Flush()
	}
	c.mut.Unlock()
	return err
}

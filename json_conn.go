package stonesthrow

import (
	"encoding/gob"
	"io"
	"log"
	"sync"
)

type jsonConnection struct {
	in io.Reader
	out io.Writer

	mut sync.Mutex
	encoder *gob.Encoder
	decoder *gob.Decoder
}

func (c jsonConnection) Receive() (interface{}, error) {
	var wrapper WrappedMessage

	c.mut.Lock()
	if c.decoder == nil {
		c.decoder = gob.NewDecoder(c.in)
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
		c.encoder = gob.NewEncoder(c.out)
	}
	err := c.encoder.Encode(WrapMessage(message))
	c.mut.Unlock()
	return err
}

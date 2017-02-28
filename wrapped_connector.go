package stonesthrow

import (
	"encoding/gob"
	"io"
	"log"
	"net"
	"sync"
)

type WrappedMessageConnector struct {
	in  io.Reader
	out io.Writer

	inChannel   chan WrappedMessage
	outChannel  chan WrappedMessage
	quitChannel chan int
	once        sync.Once
}

func reader(in io.Reader, c chan WrappedMessage, quitChannel chan int) {
	decoder := gob.NewDecoder(in)
	defer close(c)
	for {
		var wrapper WrappedMessage
		err := decoder.Decode(&wrapper)

		if err == io.EOF {
			return
		}

		// It's possible that we'll run Decode() after |in| is closed.
		if netError, ok := err.(*net.OpError); ok {
			log.Printf("Network error: Op=%s, Net=%s, Source=%s, Address=%s, Err=%s",
				netError.Op, netError.Net, netError.Source.String(),
				netError.Addr.String(), netError.Err.Error())
			return
		}

		if err != nil {
			log.Printf("Can't decode stream: %#v: %s", err, err.Error())
			return
		}
		c <- wrapper
	}
}

func writer(out io.Writer, c chan WrappedMessage, quitChannel chan int) {
	encoder := gob.NewEncoder(out)
	for message := range c {
		err := encoder.Encode(message)
		if err != nil {
			log.Printf("Error while writing: %#v: %s", err, err.Error())
			// TODO: Bail early?
		}
	}
	quitChannel <- 1
}

func (c *WrappedMessageConnector) Init() {
	pc := c
	c.once.Do(func() {
		pc.inChannel = make(chan WrappedMessage)
		pc.outChannel = make(chan WrappedMessage)
		pc.quitChannel = make(chan int)
		go reader(pc.in, pc.inChannel, pc.quitChannel)
		go writer(pc.out, pc.outChannel, pc.quitChannel)
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

func (c WrappedMessageConnector) Close() {
	close(c.outChannel)
	<- c.quitChannel
}

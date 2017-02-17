package stonesthrow

import (
	"bufio"
	"io"
	"net"
)

type Client struct {
	serverConfig Config
}

type ResponseHandler func(interface{}) error

func (c *Client) Run(serverConfig Config, request RequestMessage, handler ResponseHandler) error {
	if !serverConfig.IsValid() {
		return ConfigIncompleteError
	}
	c.serverConfig = serverConfig

	conn, err := net.Dial("tcp", serverConfig.GetListenAddress())
	if err != nil {
		return err
	}

	defer conn.Close()

	stream := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	jsConn := jsonConnection{stream: stream}

	jsConn.Send(request)

	for {
		response, err := jsConn.Receive()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		err = handler(response)
		if err != nil {
			return err
		}
	}
}

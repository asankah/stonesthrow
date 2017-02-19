package stonesthrow

import (
	"bufio"
	"io"
	"net"
)

type ResponseHandler func(interface{}) error

func RunClient(serverConfig Config, request RequestMessage, handler ResponseHandler) error {
	if !serverConfig.IsValid() {
		return ConfigIncompleteError
	}

	conn, err := net.Dial(serverConfig.Platform.Network, serverConfig.Platform.Address)
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

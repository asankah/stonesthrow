package stonesthrow

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
)

func runClientWithReaderWriter(
	clientConfig Config,
	serverConfig Config,
	request RequestMessage,
	handler chan interface{},
	reader io.Reader,
	writer io.Writer) error {

	wrappedConn := &WrappedMessageConnector{in: reader, out: writer}
	wrappedConn.Init()
	defer wrappedConn.Close()
	wrappedConn.Send(request)

	channel := Channel{conn: wrappedConn}

	reverseClientChannel := Channel{conn: localStaticConnection{ResponseSink: handler}}
	reverseClientSession := Session{
		local: clientConfig, remote: serverConfig,
		channel: reverseClientChannel, processAdder: nil}

	for {
		response, err := channel.Receive()
		if err == io.EOF || response == nil {
			return nil
		}
		if err != nil {
			return err
		}

		requestMessage, ok := response.(*RequestMessage)
		if ok {
			HandleRequestOnLocalHost(context.Background(), &reverseClientSession, *requestMessage)
		} else {
			handler <- response
		}
	}
}

func runWithLocalEndpoint(
	clientConfig Config,
	serverConfig Config,
	endpoint Endpoint,
	request RequestMessage,
	handler chan interface{}) error {

	conn, err := net.Dial(endpoint.Network, endpoint.Address)
	if err != nil {
		return err
	}

	defer conn.Close()

	return runClientWithReaderWriter(clientConfig, serverConfig, request, handler, conn, conn)
}

type localStaticConnection struct {
	ResponseSink chan interface{}
}

func (l localStaticConnection) Receive() (interface{}, error) {
	return nil, io.EOF
}

func (l localStaticConnection) Send(message interface{}) error {
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

func (l localStaticConnection) Close() {}

func runLocallyWithoutServer(serverConfig Config, request RequestMessage, handler chan interface{}) error {
	connection := localStaticConnection{ResponseSink: handler}
	channel := Channel{conn: connection}
	session := Session{local: serverConfig, remote: serverConfig, channel: channel, processAdder: nil}
	HandleRequestOnLocalHost(context.Background(), &session, request)
	return nil
}

func RunPassthroughClient(clientConfig, serverConfig Config) error {
	var endpoint Endpoint
	for _, endpoint = range serverConfig.Platform.Endpoints {
		if endpoint.Host == serverConfig.Host {
			break
		}
	}

	if endpoint.Host != serverConfig.Host {
		return InvalidPlatformError
	}

	conn, err := net.Dial(endpoint.Network, endpoint.Address)
	if err != nil {
		return err
	}

	defer conn.Close()
	var quit chan int
	quit = make(chan int)

	go func() {
		// This won't stop utnil os.Stdin is closed, which would happen
		// after this process dies. Hence we are not going to wait for
		// this call to complete.
		io.Copy(conn, os.Stdin)
	}()

	go func() {
		io.Copy(os.Stdout, conn)
		quit <- 0
	}()

	<-quit
	return nil
}

func runViaSshPassThrough(e Executor, sshTarget SshTarget, clientConfig Config, serverConfig Config,
	request RequestMessage, handler chan interface{}) error {

	ctx := context.Background()
	commandHandler, ok := GetHandlerForCommand(request.Command)
	if !ok || commandHandler.needsRevision == NEEDS_REVISION {
		// Passthrough requires that the server already have the correct BUILDER_HEAD.
		err := clientConfig.Repository.GitPushBuilderHead(ctx, e)
		if err != nil {
			return err
		}
	}

	if sshTarget.SshConfigName == "" {
		sshTarget.SshConfigName = sshTarget.HostName
	}
	sshCommand := []string{sshTarget.SshConfigName, "-T",
		filepath.Join(serverConfig.Host.StonesthrowPath, "st_client"),
		"--server", serverConfig.PlatformName,
		"--repository", serverConfig.RepositoryName,
		"--passthrough"}
	cmd := exec.CommandContext(ctx, "ssh", sshCommand...)

	writeEnd, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	readEnd, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	cmd.Start()

	return runClientWithReaderWriter(clientConfig, serverConfig, request, handler, readEnd, writeEnd)
}

func SendRequestToRemoteServer(e Executor, clientConfig Config, serverConfig Config, request RequestMessage,
	handler chan interface{}) error {
	defer close(handler)

	if !clientConfig.IsValid() || !serverConfig.IsValid() {
		return ConfigIncompleteError
	}

	if clientConfig.Host == serverConfig.Host {
		return runLocallyWithoutServer(serverConfig, request, handler)
	}

	for _, endpoint := range serverConfig.Platform.Endpoints {
		if endpoint.Host == clientConfig.Host {
			return runWithLocalEndpoint(clientConfig, serverConfig, endpoint, request, handler)
		}
	}

	for _, sshTarget := range clientConfig.Host.SshTargets {
		if sshTarget.Host == serverConfig.Host {
			return runViaSshPassThrough(e, sshTarget, clientConfig, serverConfig,
				request, handler)
		}
	}

	return NoRouteToTargetError
}

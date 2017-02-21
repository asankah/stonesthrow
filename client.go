package stonesthrow

import (
	"bufio"
	"io"
	"net"
	"os"
	"os/exec"
)

func runClientWithStream(request RequestMessage, handler chan interface{}, reader io.Reader,
	writer io.Writer) error {

	stream := bufio.NewReadWriter(bufio.NewReader(reader), bufio.NewWriter(writer))
	jsconn := jsonConnection{stream: stream}
	jsconn.Send(request)

	for {
		response, err := jsconn.Receive()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		handler <- response
	}
}

func runLocalClient(serverConfig Config, request RequestMessage, handler chan interface{}) error {
	conn, err := net.Dial(serverConfig.Platform.Network, serverConfig.Platform.Address)
	if err != nil {
		return err
	}

	defer conn.Close()

	return runClientWithStream(request, handler, conn, conn)
}

func RunPassthroughClient(serverConfig Config) error {
	conn, err := net.Dial(serverConfig.Platform.Network, serverConfig.Platform.Address)
	if err != nil {
		return err
	}

	defer conn.Close()
	var quit chan int
	quit = make(chan int)

	go func() {
		io.Copy(conn, os.Stdin)
	}()

	go func() {
		io.Copy(os.Stdout, conn)
		quit <- 0
	}()

	<-quit
	return nil
}

func runSshClient(e Executor, sshTarget SshTarget, clientConfig Config, serverConfig Config,
	request RequestMessage, handler chan interface{}) error {

	err := clientConfig.Repository.GitPushRemote(e)
	if err != nil {
		return err
	}

	if sshTarget.SshHostName == "" {
		sshTarget.SshHostName = sshTarget.HostName
	}
	sshCommand := []string{sshTarget.SshHostName, "-T", "st_client",
		"--platform", serverConfig.PlatformName,
		"--repository", serverConfig.RepositoryName,
		"--passthrough"}
	cmd := exec.Command("ssh", sshCommand...)

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

	return runClientWithStream(request, handler, readEnd, writeEnd)
}

func RunClient(e Executor, clientConfig Config, serverConfig Config, request RequestMessage,
	handler chan interface{}) error {
	defer close(handler)

	if !clientConfig.IsValid() || !serverConfig.IsValid() {
		return ConfigIncompleteError
	}

	if clientConfig.Host.Name == serverConfig.Host.Name {
		return runLocalClient(serverConfig, request, handler)
	}

	for _, sshTarget := range clientConfig.Host.SshTargets {
		if sshTarget.Host.Name == serverConfig.Host.Name {
			return runSshClient(e, sshTarget, clientConfig, serverConfig,
				request, handler)
		}
	}
	return NoRouteToTargetError
}

package stonesthrow

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"io"
	"net"
	"os"
	"os/exec"
	"time"
)

func getCredentialsForClient(client_config Config) (credentials.TransportCredentials, error) {
	if client_config.Host.Certificates == nil || client_config.Host.Certificates.RootCert == nil {
		return nil, NewConfigIncompleteError("Client does not specify a CA certificate")
	}

	return credentials.NewClientTLSFromFile(client_config.Host.Certificates.RootCert.CertificateFile, "")
}

func connectToLocalEndpoint(ctx context.Context, client_config, server_config Config, endpoint Endpoint) (*grpc.ClientConn, error) {
	creds, err := getCredentialsForClient(client_config)
	if err != nil {
		return nil, err
	}

	return grpc.DialContext(ctx, endpoint.Address,
		grpc.WithAuthority(server_config.Host.Name),
		grpc.WithTransportCredentials(creds))
}

func connectViaSsh(ctx context.Context, client_config, server_config Config, remote RemoteTransportConfig) (*grpc.ClientConn, error) {
	creds, err := getCredentialsForClient(client_config)
	if err != nil {
		return nil, err
	}

	command_line := remote.GetCommand(&server_config)
	cmd := exec.CommandContext(ctx, command_line[0], command_line[1:]...)

	writeEnd, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	readEnd, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	cmd.Start()

	return grpc.DialContext(ctx, server_config.Host.Name,
		grpc.WithAuthority(server_config.Host.Name),
		grpc.WithTransportCredentials(creds),
		grpc.WithDialer(func(string, time.Duration) (net.Conn, error) {
			return PipeConnection{reader: readEnd, writer: writeEnd}, nil
		}))
}

func ConnectTo(ctx context.Context, client_config, server_config Config) (*grpc.ClientConn, error) {
	if !client_config.IsValid() || !server_config.IsValid() {
		return nil, NewConfigIncompleteError("Client or server configuration is invalid")
	}

	// If the server has an endpoint on the client machine, then talk to that endpoint.
	for _, endpoint := range server_config.Platform.Endpoints {
		if endpoint.Host == client_config.Host {
			return connectToLocalEndpoint(ctx, client_config, server_config, endpoint)
		}
	}

	// If we can ssh directly to the server, then do so.
	for _, remote := range client_config.Host.Remotes {
		if remote.Host == server_config.Host {
			return connectViaSsh(ctx, client_config, server_config, *remote)
		}
	}

	// Finally, if we can ssh to a host that has an endpoint for the target server, do so.
	for _, remote := range client_config.Host.Remotes {
		for _, endpoint := range server_config.Platform.Endpoints {
			if endpoint.Host == remote.Host {
				return connectViaSsh(ctx, client_config, server_config, *remote)
			}
		}
	}

	// We don't try any harder than this.
	return nil, NewNoRouteToTargetError("Target server %s", server_config.Host.Name)
}

func RunPassthroughClient(client_config, server_config Config) error {
	var endpoint Endpoint
	for _, endpoint = range server_config.Platform.Endpoints {
		if endpoint.Host == server_config.Host {
			break
		}
	}

	if endpoint.Host != server_config.Host {
		return NewInvalidPlatformError(
			"Currently we only support a single hop for SSH passthrough. " +
				"Hence the server must host the endpoint.")
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

package stonesthrow

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
)

func getCredentialsForServer(server_config Config) (credentials.TransportCredentials, error) {
	if server_config.Host.Certificates == nil || server_config.Host.Certificates.ServerCert == nil {
		return nil, NewConfigIncompleteError("can't locate server key and certificate")
	}

	return credentials.NewServerTLSFromFile(server_config.Host.Certificates.ServerCert.CertificateFile,
		server_config.Host.Certificates.ServerCert.KeyFile)
}

func RunServer(Config Config) error {
	service_host_server := ServiceHostServerImpl{Config: Config}
	repository_host_server := RepositoryHostServerImpl{Config: Config, Repository: Config.Repository,
		ProcessAdder: &service_host_server}
	platform_build_server := PlatformBuildHostServerImpl{Config: Config, ProcessAdder: &service_host_server}

	creds, err := getCredentialsForServer(Config)
	if err != nil {
		return err
	}

	endpoint := Config.Platform.EndpointFor(Config.Host)
	if endpoint == nil {
		return NewInvalidPlatformError("platform has no endpoint here")
	}
	listener, err := net.Listen(endpoint.Network, endpoint.Address)
	if err != nil {
		return err
	}

	server := grpc.NewServer(grpc.Creds(creds))
	RegisterServiceHostServer(server, &service_host_server)
	RegisterRepositoryHostServer(server, &repository_host_server)
	RegisterPlatformBuildHostServer(server, &platform_build_server)
	service_host_server.Server = server

	return server.Serve(listener)
}

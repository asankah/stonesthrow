package stonesthrow

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
)

func getCredentialsForHost(host_config *HostConfig) (credentials.TransportCredentials, error) {
	if host_config.Certificates == nil || host_config.Certificates.ServerCert == nil {
		return nil, NewConfigIncompleteError("can't locate server key and certificate")
	}

	return credentials.NewServerTLSFromFile(host_config.Certificates.ServerCert.CertificateFile,
		host_config.Certificates.ServerCert.KeyFile)
}

func RunServer(Config Config) error {
	service_host_server := ServiceHostServerImpl{Config: Config}
	repository_host_server := RepositoryHostServerImpl{Host: Config.Host, ProcessAdder: &service_host_server}
	platform_build_server := BuildHostServerImpl{Host: Config.Host, ProcessAdder: &service_host_server}

	creds, err := getCredentialsForHost(Config.Host)
	if err != nil {
		return err
	}

	endpoint := Config.Host.GetEndpointOnHost(Config.Host)
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
	RegisterBuildHostServer(server, &platform_build_server)
	service_host_server.Server = server

	return server.Serve(listener)
}

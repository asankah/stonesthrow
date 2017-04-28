package stonesthrow

import (
	grpc "google.golang.org/grpc"
)

type JobEventServer interface {
	Send(*JobEvent) error
	grpc.ServerStream
}

package stonesthrow

import (
	"google.golang.org/grpc"
)

type JobEventReceiver interface {
	Recv() (*JobEvent, error)
	grpc.ClientStream
}

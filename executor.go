package stonesthrow

import (
	"context"
)

type Executor interface {
	ExecuteSilently(ctx context.Context, workdir string, command ...string) (string, error)
	ExecuteWithOutput(ctx context.Context, workdir string, command ...string) (string, error)
	Execute(ctx context.Context, workdir string, command ...string) error
}

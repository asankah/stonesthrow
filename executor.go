package stonesthrow

import (
	"context"
)

type Executor interface {
	// ExecutePassthrough runs a command and passes stdout to an underlying
	// output channel. The working directory is implicit.
	ExecutePassthrough(ctx context.Context, command ...string) error

	// ExecuteNoStream runs a command and returns stdout as a string. The
	// working directory is implicit.
	ExecuteNoStream(ctx context.Context, command ...string) (string, error)

	// Execute runs a command and returns stdout as a string. The output is
	// also passed down to an underlying output channel. The working
	// directory is implicit.
	Execute(ctx context.Context, command ...string) (string, error)

	// ExecuteInWorkDirPassthrough runs a command and passes stdout to an
	// underlying output channel.
	ExecuteInWorkDirPassthrough(workdir string, ctx context.Context, command ...string) error

	// ExecuteInWorkDir runs a command and returns stdout as a string. The
	// output is also passed down to an underlying output channel.
	ExecuteInWorkDir(workdir string, ctx context.Context, command ...string) (string, error)

	// ExecuteInWorkDirNoStream runs a command and returns stdout as a
	// string.
	ExecuteInWorkDirNoStream(workdir string, ctx context.Context, command ...string) (string, error)
}

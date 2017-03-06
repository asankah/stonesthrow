package stonesthrow

import (
	"errors"
	"fmt"
)

var (
	ConfigIncompleteError          = errors.New("configuration incomplete")
	DepsChangedError               = errors.New("DEPS changed")
	EmptyCommandError              = errors.New("empty command")
	ExternalCommandFailedError     = errors.New("external command failed")
	InvalidArgumentError           = errors.New("invalid argument")
	InvalidWrappedMessageTypeError = errors.New("invalid message type during unwrap")
	InvalidMessageTypeError        = errors.New("invalid message type during wrap")
	InvalidPlatformError           = errors.New("invalid platform")
	NoRouteToTargetError           = errors.New("no route to target host")
	NoTargetError                  = errors.New("no target specified")
	NoUpstreamError                = errors.New("no upstream configured for this repository")
	OnlyOnMasterError              = errors.New("command is only available on master")
	TimedOutError                  = errors.New("timed out")
	UnmergedChangesExistError      = errors.New("working directory has unmerged changes")
	UnrecognizedResponseError      = errors.New("server sent an unrecognized response")
	WorkTreeDirtyError             = errors.New("working directory is dirty")
	FailedToPushGitBranchError     = errors.New("failed to push git branch")
	EndpointNotFoundError          = errors.New("endpoint not found")
	NothingToDoError               = errors.New("nothing to do")
)

type ConfigError struct {
	ConfigFile  string
	ErrorString string
}

func (c ConfigError) Error() string {
	return fmt.Sprintf("Configuration error: %s: %s", c.ConfigFile, c.ErrorString)
}

package stonesthrow

import (
	"errors"
)

var (
	ExternalCommandFailedError = errors.New("external command failed")
	EmptyCommandError          = errors.New("empty command")
	TimedOutError              = errors.New("timed out")
	NoTargetError              = errors.New("no target specified")
	InvalidMessageType         = errors.New("invalid message type")
	InvalidArgumentError       = errors.New("invalid argument")
	OnlyOnMasterError          = errors.New("command is only available on master")
	UnrecognizedResponseError  = errors.New("server sent an unrecognized response")
	WorkTreeDirtyError         = errors.New("working directory is dirty")
	ConfigIncompleteError      = errors.New("configuration incomplete")
	UnmergedChangesExistError  = errors.New("working directory has unmerged changes")
)

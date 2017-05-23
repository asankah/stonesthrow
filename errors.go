package stonesthrow

import (
	"fmt"
	"runtime/debug"
)

type ErrorWithDetails struct {
	ErrorClass string
	Details    string
	Stack      []byte
}

func (e ErrorWithDetails) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.ErrorClass, e.Details, e.Stack)
}

func NewErrorClass(class string) (func(string, ...interface{}) error, func(error) bool) {
	return func(details string, extra ...interface{}) error {
			return ErrorWithDetails{
				ErrorClass: class,
				Details:    fmt.Sprintf(details, extra...),
				Stack:      debug.Stack()}
		},
		func(err error) bool {
			error_with_details, ok := err.(ErrorWithDetails)
			return ok && error_with_details.ErrorClass == class
		}
}

var (
	NewConfigurationError, IsConfigurationError                         = NewErrorClass("configuration error")
	NewConfigIncompleteError, IsConfigIncompleteError                   = NewErrorClass("configuration incomplete")
	NewDepsChangedError, IsDepsChangedError                             = NewErrorClass("DEPS changed")
	NewEmptyCommandError, IsEmptyCommandError                           = NewErrorClass("empty command")
	NewExternalCommandFailedError, IsExternalCommandFailedError         = NewErrorClass("external command failed")
	NewInvalidArgumentError, IsInvalidArgumentError                     = NewErrorClass("invalid argument")
	NewInvalidWrappedMessageTypeError, IsInvalidWrappedMessageTypeError = NewErrorClass("invalid message type during unwrap")
	NewInvalidMessageTypeError, IsInvalidMessageTypeError               = NewErrorClass("invalid message type during wrap")
	NewInvalidPlatformError, IsInvalidPlatformError                     = NewErrorClass("invalid platform")
	NewInvalidRepositoryError, IsInvalidRepositoryError                 = NewErrorClass("invalid repository")
	NewNoRouteToTargetError, IsNoRouteToTargetError                     = NewErrorClass("no route to target host")
	NewNoTargetError, IsNoTargetError                                   = NewErrorClass("no target specified")
	NewNoUpstreamError, IsNoUpstreamError                               = NewErrorClass("no upstream configured for this repository")
	NewOnlyOnMasterError, IsOnlyOnMasterError                           = NewErrorClass("command is only available on master")
	NewTimedOutError, IsTimedOutError                                   = NewErrorClass("timed out")
	NewUnmergedChangesExistError, IsUnmergedChangesExistError           = NewErrorClass("working directory has unmerged changes")
	NewUnrecognizedResponseError, IsUnrecognizedResponseError           = NewErrorClass("server sent an unrecognized response")
	NewWorkTreeDirtyError, IsWorkTreeDirtyError                         = NewErrorClass("working directory is dirty")
	NewFailedToPushGitBranchError, IsFailedToPushGitBranchError         = NewErrorClass("failed to push git branch")
	NewEndpointNotFoundError, IsEndpointNotFoundError                   = NewErrorClass("endpoint not found")
	NewNothingToDoError, IsNothingToDoError                             = NewErrorClass("nothing to do")
	NewConnectionError, IsConnectionError                               = NewErrorClass("connection failed")
)

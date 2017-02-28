package stonesthrow

type WrappedMessage struct {
	Output       *TerminalOutputMessage `json:"output,omitempty"`
	Info         *InfoMessage           `json:"info,omitempty"`
	Error        *ErrorMessage          `json:"error,omitempty"`
	BeginCommand *BeginCommandMessage   `json:"begin,omitempty"`
	EndCommand   *EndCommandMessage     `json:"end,omitempty"`
	CommandList  *CommandListMessage    `json:"ls,omitempty"`
	Request      *RequestMessage        `json:"req,omitempty"`
	JobList      *JobListMessage        `json:"jobs,omitempty"`
}

func WrapMessage(message interface{}) (WrappedMessage, error) {
	var wrapper WrappedMessage
	switch t := message.(type) {
	case TerminalOutputMessage:
		wrapper.Output = &t

	case InfoMessage:
		wrapper.Info = &t

	case ErrorMessage:
		wrapper.Error = &t

	case BeginCommandMessage:
		wrapper.BeginCommand = &t

	case EndCommandMessage:
		wrapper.EndCommand = &t

	case CommandListMessage:
		wrapper.CommandList = &t

	case RequestMessage:
		wrapper.Request = &t

	case JobListMessage:
		wrapper.JobList = &t

	default:
		return wrapper, InvalidMessageType
	}

	return wrapper, nil
}

func UnwrapMessage(wrapper WrappedMessage) (interface{}, error) {
	switch {
	case wrapper.Output != nil:
		return wrapper.Output, nil

	case wrapper.BeginCommand != nil:
		return wrapper.BeginCommand, nil

	case wrapper.EndCommand != nil:
		return wrapper.EndCommand, nil

	case wrapper.Info != nil:
		return wrapper.Info, nil

	case wrapper.Error != nil:
		return wrapper.Error, nil

	case wrapper.CommandList != nil:
		return wrapper.CommandList, nil

	case wrapper.JobList != nil:
		return wrapper.JobList, nil

	case wrapper.Request != nil:
		return wrapper.Request, nil
	}
	return nil, InvalidMessageType
}

package stonesthrow

import (
	"context"
	"google.golang.org/grpc"
	"os"
	"time"
)

type ProcessAdder interface {
	AddProcess(command []string, process *os.Process)
	RemoveProcess(process *os.Process, state *os.ProcessState)
}

type ServiceHostServerImpl struct {
	Config     Config
	ProcessMap map[int]*BuilderJob
	Server     *grpc.Server
}

func (h *ServiceHostServerImpl) Ping(ctx context.Context, po *PingOptions) (*PingResult, error) {
	return &PingResult{Pong: po.GetPing()}, nil
}

func (h *ServiceHostServerImpl) ListJobs(ctx context.Context, rs *RepositoryState) (*BuilderJobs, error) {
	return nil, NewNothingToDoError("not implemented")
}

func (h *ServiceHostServerImpl) KillJobs(bj *BuilderJobs, s ServiceHost_KillJobsServer) error {
	return NewNothingToDoError("not implemented")
}

func (h *ServiceHostServerImpl) Shutdown(rs *RepositoryState, s ServiceHost_ShutdownServer) error {
	go func() {
		h.Server.GracefulStop()
	}()
	return nil
}

func (h *ServiceHostServerImpl) SelfUpdate(rs *RepositoryState, s ServiceHost_SelfUpdateServer) error {
	return NewNothingToDoError("not implemented")
}

func (h *ServiceHostServerImpl) AddProcess(command []string, process *os.Process) {
	bj := BuilderJob{
		Command: &ShellCommand{
			Command:   command,
			Directory: "",
			Host:      h.Config.Host.Name},
		State: &RunState{
			StartTime: NewTimestampFromTime(time.Now()),
			Running:   true}}
	if h.ProcessMap == nil {
		h.ProcessMap = make(map[int]*BuilderJob)
	}
	h.ProcessMap[process.Pid] = &bj
}

func (h *ServiceHostServerImpl) RemoveProcess(process *os.Process, state *os.ProcessState) {
	bj, ok := h.ProcessMap[process.Pid]
	if !ok {
		return
	}
	bj.State.Running = false
	bj.State.EndTime = NewTimestampFromTime(time.Now())
	bj.SystemTime = NewDurationFromDuration(state.SystemTime())
	bj.UserTime = NewDurationFromDuration(state.UserTime())
}

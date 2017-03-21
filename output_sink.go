package stonesthrow

import "io"

type OutputSink interface {
	OnGitRepositoryInfo(*GitRepositoryInfo) error
	OnTargetList(*TargetList) error
	OnBuilderJobs(*BuilderJobs) error
	OnJobEvent(*JobEvent) error
	OnPong(*PingResult) error

	Drain(JobEventReceiver) error
	DrainReader(CommandOutputEvent, io.Reader) error

	Send(*JobEvent) error
}

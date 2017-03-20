package stonesthrow

type OutputSink interface {
	SendRepositoryInfo(*GitRepositoryInfo) error
	SendTargetList(*TargetList) error
	SendBuilderJobs(*BuilderJobs) error
	JobEventSender
}

package stonesthrow

import (
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/timestamp"
	"time"
)

func NewTimestampFromTime(t time.Time) *timestamp.Timestamp {
	t = t.UTC()
	return &timestamp.Timestamp{
		Seconds: t.Unix(),
		Nanos:   int32(t.UnixNano() - t.Unix()*time.Second.Nanoseconds())}
}

func NewDurationFromDuration(d time.Duration) *duration.Duration {
	seconds := d.Nanoseconds() / time.Second.Nanoseconds()
	return &duration.Duration{
		Seconds: seconds,
		Nanos:   int32(d.Nanoseconds() - seconds*time.Second.Nanoseconds())}
}

func TimestampNow() *timestamp.Timestamp {
	return NewTimestampFromTime(time.Now())
}

func BranchListFromGitRepositoryInfo_Branch(r []*GitRepositoryInfo_Branch) []string {
	s := []string{}
	for _, b := range r {
		s = append(s, b.GetName())
	}
	return s
}

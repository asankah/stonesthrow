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
		Nanos:   t.UnixNano() - t.Unix()*1000000000}
}

func TimestampNow() *timestamp.Timestamp {
	return NewTimestampFromTime(time.Now())
}

func BranchListFromGitRepositoryInfo_Branch(r []GitRepositoryInfo_Branch) []string {
	s := []string{}
	for _, b := range r {
		s = append(s, b.GetName())
	}
	return s
}

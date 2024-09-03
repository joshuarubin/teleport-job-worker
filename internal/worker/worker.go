package worker

import (
	"context"
	"io"

	"go.jetify.com/typeid"
)

type JobPrefix struct{}

func (JobPrefix) Prefix() string { return "job" }

type JobID struct {
	typeid.TypeID[JobPrefix]
}

type (
	UserID    string
	ExitCode  int
	JobStatus int
)

const (
	JobStatusUnspecified JobStatus = iota
	JobStatusRunning
	JobStatusComplete
	JobStatusError
)

type Worker interface {
	StartJob(ctx context.Context, userID UserID, command string, args ...string) (JobID, error)
	StartJobChild(ctx context.Context, command string, args ...string) error
	StopJob(ctx context.Context, userID UserID, jobID JobID) (ExitCode, error)
	JobStatus(ctx context.Context, userID UserID, jobID JobID) (JobStatus, ExitCode, error)
	JobOutput(ctx context.Context, userID UserID, jobID JobID) (io.ReadCloser, error)
}

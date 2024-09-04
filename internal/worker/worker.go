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
	UserID   string
	ExitCode int
)

func NewJobID() (JobID, error) {
	return typeid.New[JobID]()
}

func (id UserID) String() string {
	return string(id)
}

func (c ExitCode) Int() int {
	return int(c)
}

//go:generate stringer -type=JobStatus -trimprefix=JobStatus
type JobStatus int

// NOTE: keep this synced with jobworker.proto:JobStatus
const (
	JobStatusUnspecified JobStatus = iota
	JobStatusRunning               // the job has been started
	JobStatusComplete              // the job completed successfully on its own
	JobStatusStopped               // the job completed after being manually signaled to stop
)

type JobStatusResponse struct {
	Status JobStatus

	// ExitCode is optional since, in the case the job is still running or was
	// killed, it may not have one
	ExitCode *ExitCode
}

type Worker interface {
	StartJob(ctx context.Context, userID UserID, command string, args ...string) (JobID, error)
	StartJobChild(ctx context.Context, command string, args ...string) error
	StopJob(ctx context.Context, userID UserID, jobID JobID) error
	JobStatus(ctx context.Context, userID UserID, jobID JobID) (*JobStatusResponse, error)
	JobOutput(ctx context.Context, userID UserID, jobID JobID) (io.ReadCloser, error)
}

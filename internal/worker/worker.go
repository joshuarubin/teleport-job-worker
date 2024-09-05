package worker

import (
	"context"
	"io"

	"github.com/joshuarubin/teleport-job-worker/internal/job"
	"github.com/joshuarubin/teleport-job-worker/internal/user"
)

type Worker interface {
	StartJob(ctx context.Context, userID user.ID, command string, args ...string) (job.ID, error)
	StartJobChild(ctx context.Context, command string, args ...string) error
	StopJob(ctx context.Context, userID user.ID, jobID job.ID) error
	JobStatus(ctx context.Context, userID user.ID, jobID job.ID) (*job.StatusResponse, error)
	JobOutput(ctx context.Context, userID user.ID, jobID job.ID) (io.ReadCloser, error)
}

package jobworker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/joshuarubin/teleport-job-worker/internal/config"
	"github.com/joshuarubin/teleport-job-worker/internal/job"
	"github.com/joshuarubin/teleport-job-worker/internal/user"
	"github.com/joshuarubin/teleport-job-worker/internal/worker"
)

// ReexecCommand contains the necessary configuration for the JobWorker to be
// able to call exec.Command() and expect that the current binary will be
// reexecuted, the JobWorker will be reinstantiated, and
// JobWorker.StartJobChild() will be called with the remaining arguments passed
// in as command and args
type ReexecCommand struct {
	Command string   // often "/proc/self/exe"
	Args    []string // usually a command that puts this binary in a different "mode", e.g. "child" or "helper"
	Env     []string // additional env variables, in the form of "key=value", to be added to the current os.Environ()
}

type cgroup struct {
	cfg        *config.Config
	createOnce sync.Once //nolint:unused,nolintlint
	createErr  error     //nolint:unused,nolintlint
	name       string    //nolint:unused,nolintlint
}

// JobWorker is an implementation of the Worker interface
type JobWorker struct {
	cfg    *config.Config
	reexec *ReexecCommand

	cgroup *cgroup

	mu   sync.RWMutex
	jobs map[job.ID]*job.Job
}

// ensure JobWorker implements the Worker interface
var _ worker.Worker = (*JobWorker)(nil)

// New creates a new JobWorker
func New(cfg *config.Config, reexec *ReexecCommand) *JobWorker {
	return &JobWorker{
		cfg:    cfg,
		cgroup: &cgroup{cfg: cfg},
		reexec: reexec,
		jobs:   map[job.ID]*job.Job{},
	}
}

// StartJob executes command, with optional args, in a new pid, mount and
// network namespace. It also creates a new cgroup and applies cpu.max,
// memory.max and io.max limits as provided in cfg when JobWorker was created.
// The provided context will kill the process if it becomes done, so don't use
// an ephemeral context like one for a particular request. The userID is an
// opaque value that is used for authorization of later requests. Only matching
// userIDs will be able to Stop or get the Status or Output of a job. Returns
// the opaque job.ID that is required for subsequent operations with the job.
func (w *JobWorker) StartJob(ctx context.Context, userID user.ID, command string, args ...string) (job.ID, error) {
	cmdArgs := w.reexec.Args
	cmdArgs = append(cmdArgs, command)
	cmdArgs = append(cmdArgs, args...)

	j, err := job.New(
		ctx,
		w.cfg,
		userID,
		w.reexec.Command,
		cmdArgs,
		w.reexec.Env,
	)
	if err != nil {
		return job.ID{}, err
	}

	if err = j.Start(); err != nil {
		return job.ID{}, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.jobs[j.ID()] = j

	return j.ID(), nil
}

// StartJobChild is called when this binary is reexecuted with the new
// namespaces applied. It should be the only method called by the binary in that
// circumstance and should never be called in any other situation. It will
// create a new cgroup with cpu, memory and io limits applied, then it will
// remount /proc and finally it will execute the command, with optional args.
func (w *JobWorker) StartJobChild(_ context.Context, command string, args ...string) error {
	cmd, err := exec.LookPath(command)
	if err != nil {
		err = fmt.Errorf("LookPath error: %w", err)
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	if err = w.prepJobEnv(); err != nil {
		err = fmt.Errorf("prepJobEnv error: %w", err)
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	args = append([]string{cmd}, args...)
	if err = syscall.Exec(cmd, args, os.Environ()); err != nil {
		err = fmt.Errorf("syscall.Exec error: %w", err)
		fmt.Fprintln(os.Stderr, err)
		return err
	}

	return nil
}

// ErrJobNotFound is returned when trying to stop, get status or get output of a
// job that doesn't exist or that the user is not authorized for.
var ErrJobNotFound = errors.New("job not found")

func (w *JobWorker) getJob(userID user.ID, jobID job.ID) (*job.Job, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	job, ok := w.jobs[jobID]
	if !ok || job.UserID() != userID {
		return nil, ErrJobNotFound
	}

	return job, nil
}

// StopJob kills the job identified by jobID. If the job does not exist, or if
// the user is not authorized, ErrJobNotFound will be returned.
func (w *JobWorker) StopJob(_ context.Context, userID user.ID, jobID job.ID) error {
	job, err := w.getJob(userID, jobID)
	if err != nil {
		return err
	}

	return job.Stop()
}

// JobStatus will return the JobStatus and optional ExitCode from the job. The
// ExitCode will not exist if the job is still running, or if it exited
// uncleanly, e.g. because it was killed by a signal. If the job does not exist,
// or if the user is not authorized, ErrJobNotFound will be returned.
func (w *JobWorker) JobStatus(_ context.Context, userID user.ID, jobID job.ID) (*job.StatusResponse, error) {
	j, err := w.getJob(userID, jobID)
	if err != nil {
		return nil, err
	}

	resp := job.StatusResponse{
		Status: j.Status(),
		Error:  j.Error(),
	}

	if code, ok := j.ExitCode(); ok {
		resp.ExitCode = &code
	}

	return &resp, nil
}

// JobOutput returns an io.ReadCloser that can be used to stream the output of a
// job. Creating multiple readers for a single job is safe. It is the
// responsibility of the caller to close the reader when done to free resources.
// If the job does not exist, or if the user is not authorized, ErrJobNotFound
// will be returned.
func (w *JobWorker) JobOutput(_ context.Context, userID user.ID, jobID job.ID) (io.ReadCloser, error) {
	j, err := w.getJob(userID, jobID)
	if err != nil {
		return nil, err
	}
	return j.NewOutputReader(), nil
}

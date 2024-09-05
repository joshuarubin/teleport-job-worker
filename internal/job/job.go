package job

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/joshuarubin/teleport-job-worker/internal/config"
	"github.com/joshuarubin/teleport-job-worker/internal/safebuffer"
	"github.com/joshuarubin/teleport-job-worker/internal/user"
)

// Job represents a system process
type Job struct {
	cfg    *config.Config
	id     ID
	userID user.ID
	cmd    *exec.Cmd
	buf    *safebuffer.Buffer

	startOnce sync.Once

	stopOnce sync.Once
	stopErr  atomic.Value

	status atomic.Uint32

	done   chan struct{}
	cmdErr error // only safe to read after done has closed
}

// New creates, but does not start a new job
func New(
	ctx context.Context,
	cfg *config.Config,
	userID user.ID,
	command string,
	args []string,
	env []string,
) (*Job, error) {
	id, err := NewID()
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})

	j := Job{
		cfg:    cfg,
		id:     id,
		userID: userID,
		done:   done,
		cmd:    exec.CommandContext(ctx, command, args...),
	}

	j.buf = safebuffer.New(ctx, j.done)
	j.cmd.Stdout = j.buf
	j.cmd.Stderr = j.buf

	j.cmd.SysProcAttr = sysProcAttr()

	j.cmd.Env = os.Environ()
	j.cmd.Env = append(j.cmd.Env, env...)
	j.setStatus(StatusNotStarted)

	return &j, nil
}

var ErrAlreadyStarted = errors.New("already started")

// Start the job process. If Start() is called more than once ErrAlreadyStarted
// will be returned.
func (j *Job) Start() error {
	var err error
	var started bool
	j.startOnce.Do(func() {
		j.setStatus(StatusRunning)
		err = j.cmd.Start()
		started = true

		go func() {
			defer func() {
				j.setStatus(StatusComplete)
				close(j.done)
			}()
			j.cmdErr = j.cmd.Wait()
		}()
	})

	if !started {
		return ErrAlreadyStarted
	}

	return err
}

// ID returns the job ID
func (j *Job) ID() ID {
	return j.id
}

// UserID returns the job ID
func (j *Job) UserID() user.ID {
	return j.userID
}

// NewOutputReader returns an io.ReadCloser that can be used to stream the
// output of the job from the time it started. It is the caller's responsibility
// to close it to free allocated resources.
func (j *Job) NewOutputReader() io.ReadCloser {
	return j.buf.NewReader()
}

// Done returns a channel that will be closed when the job completes
func (j *Job) Done() <-chan struct{} {
	return j.done
}

// setStatus sets the job status. status can only move to higher values:
// not_started -> running -> complete -> stopped
func (j *Job) setStatus(st Status) {
	for {
		cur := j.Status()
		if st <= cur {
			return
		}

		if j.status.CompareAndSwap(uint32(cur), uint32(st)) { //nolint:gosec,nolintlint
			return
		}
	}
}

// Status returns the job's status
func (j *Job) Status() Status {
	return Status(j.status.Load())
}

// IsRunning returns whether or not the job process is still running
func (j *Job) IsRunning() bool {
	select {
	case <-j.done:
		return false
	default:
		return true
	}
}

// Error returns the error returned by exec.Command. It will return nil if the
// command is still running.
func (j *Job) Error() error {
	if j.IsRunning() {
		return nil
	}
	return j.cmdErr
}

// ExitCode returns the process's exit code and whether or not it is valid. If
// the process is still running, the exit code is not valid. If the process
// completed successfully, the exit code is assumed to be 0. If the process
// returned any exit code, it will be used. In the case the process was stopped
// or signaled in some other way, it may have completed without a valid exit
// code.
func (j *Job) ExitCode() (ExitCode, bool) {
	if j.IsRunning() {
		return 0, false
	}

	// if the job completed on its own and there is no error then its exit code
	// is assumed to be 0
	if j.Status() == StatusComplete && j.cmdErr == nil {
		return 0, true
	}

	// if there was an error that has an exit code, obviously, use that as the
	// exit code
	var eerr *exec.ExitError
	if errors.As(j.cmdErr, &eerr) {
		return ExitCode(eerr.ExitCode()), true
	}

	// there may or may not be an error, but it doesn't have an exit code. this
	// could be due to the fact that the job was killed with a signal. in any
	// event, there's no usable exit code
	return 0, false
}

// Stop the process. Returns any error from the signaling of the process, not
// the error or exit code that the process itself finished with. Repeated calls
// to Stop() will not signal the process again and will always return the same
// value. If the process had already completed when first called, Stop() does
// nothing.
func (j *Job) Stop() error {
	j.stopOnce.Do(func() {
		if !j.IsRunning() {
			return
		}

		var err error

		if err = j.cmd.Process.Kill(); errors.Is(err, os.ErrProcessDone) {
			// there was a race, job wasn't done when Stop() was first called,
			// but it was by the time the signal was sent
			return
		}
		if err != nil {
			j.stopErr.Store(err)
			return
		}

		<-j.done
		j.setStatus(StatusStopped)
	})

	if err, ok := j.stopErr.Load().(error); ok {
		return err
	}
	return nil
}

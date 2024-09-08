package job

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/joshuarubin/teleport-job-worker/pkg/safebuffer"
)

// Job represents a system process
type Job struct {
	id     ID
	userID UserID
	cmd    *exec.Cmd
	buf    *safebuffer.Buffer

	startOnce sync.Once
	startErr  error

	stopOnce sync.Once
	stopErr  error

	status atomic.Uint32

	done chan struct{}

	// these values are only safe to read after done has closed
	cmdErr   error
	exitCode *ExitCode
}

// New creates, but does not start a new job
func New(
	userID UserID,
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
		id:     id,
		userID: userID,
		done:   done,
		cmd:    exec.Command(command, args...),
	}

	j.buf = safebuffer.New(j.done)
	j.cmd.Stdout = j.buf
	j.cmd.Stderr = j.buf

	j.cmd.SysProcAttr = sysProcAttr()

	j.cmd.Env = append(os.Environ(), env...)
	j.setStatus(StatusNotStarted)

	return &j, nil
}

// ErrAlreadyStarted is returned when trying to start a job that has already
// been started
var ErrAlreadyStarted = errors.New("already started")

// Start the job process. If Start() is called more than once ErrAlreadyStarted
// will be returned.
func (j *Job) Start() error {
	var started bool
	j.startOnce.Do(func() {
		if j.startErr = j.cmd.Start(); j.startErr != nil {
			return
		}
		j.setStatus(StatusRunning)
		started = true
		go j.wait()
	})
	if j.startErr != nil {
		return j.startErr
	}
	if !started {
		return ErrAlreadyStarted
	}
	return nil
}

func (j *Job) wait() {
	defer func() {
		j.setStatus(StatusCompleted)
		close(j.done)
	}()

	j.cmdErr = j.cmd.Wait()

	var ec ExitCode

	if j.cmdErr == nil {
		j.exitCode = &ec
		return
	}

	var eerr *exec.ExitError
	if errors.As(j.cmdErr, &eerr) {
		ec = ExitCode(eerr.ExitCode())
		j.exitCode = &ec
	}
}

// ID returns the job ID
func (j *Job) ID() ID {
	return j.id
}

// UserID returns the job ID
func (j *Job) UserID() UserID {
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

// ExitCode returns the process's exit code. If the process is still running,
// the exit code is nil. If the process completed successfully, the exit code
// is assumed to be 0. If the process returned any exit code, it will be used.
// In the case the process was stopped or signaled in some other way, it may
// have completed without a valid exit code and may be nil.
func (j *Job) ExitCode() *ExitCode {
	if j.IsRunning() {
		return nil
	}
	return j.exitCode
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

		err := j.cmd.Process.Kill()
		if errors.Is(err, os.ErrProcessDone) {
			// there was a race, job wasn't done when Stop() was first called,
			// but it was by the time the signal was sent
			return
		}
		if err != nil {
			j.stopErr = err
			return
		}

		<-j.done
		j.setStatus(StatusStopped)
	})

	return j.stopErr
}

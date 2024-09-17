package job

import (
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/joshuarubin/teleport-job-worker/pkg/safebuffer"
)

// Job represents a system process
type Job struct {
	id     ID
	userID UserID
	cmd    *exec.Cmd
	buf    *safebuffer.Buffer
	status Status
	done   chan struct{}

	// these values are only safe to read after done has closed
	cmdErr   error
	exitCode *ExitCode
}

var (
	// ErrUserIDRequired is returned by New if user id is
	// empty
	ErrUserIDRequired = errors.New("user id is required")

	// ErrCommandRequired is returned by New if command is
	// empty
	ErrCommandRequired = errors.New("command is required")
)

// New creates, but does not start a new job
func New(
	userID UserID,
	command string,
	args []string,
	env []string,
) (*Job, error) {
	if userID == "" {
		return nil, ErrUserIDRequired
	}
	if command == "" {
		return nil, ErrCommandRequired
	}

	id, err := NewID()
	if err != nil {
		return nil, err
	}

	j := Job{
		id:     id,
		userID: userID,
		done:   make(chan struct{}),
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

// Start the job process
func (j *Job) Start() error {
	if err := j.cmd.Start(); err != nil {
		j.setStatus(StatusStartError)
		return err
	}
	j.setStatus(StatusRunning)
	go j.wait()
	return nil
}

// wait for the command to finish. sets the error returned by the command, if
// any, extracts any exit code, sets status to completed and closes the done
// channel
func (j *Job) wait() {
	defer func() {
		j.setStatus(StatusCompleted)
		close(j.done)
	}()

	// NOTE: cmdErr and exitCode must be written before closing done to avoid
	// races

	j.cmdErr = j.cmd.Wait()

	var ec ExitCode
	if j.cmdErr == nil {
		// nil error implies 0 exit code
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
// not_started -> running -> completed -> stopped
func (j *Job) setStatus(st Status) {
	if st <= j.status {
		return
	}
	j.status = st
}

// Status returns the job's status
func (j *Job) Status() Status {
	return j.status
}

// isDone returns whether or not the job process is still running
func (j *Job) isDone() bool {
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
	if j.isDone() {
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
	if j.isDone() {
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
	if err := j.cmd.Process.Kill(); err != nil {
		return err
	}
	<-j.done
	j.setStatus(StatusStopped)
	return nil
}

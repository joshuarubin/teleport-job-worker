package job

// ExitCode represents the exit code returned by a job
type ExitCode int

// Int returns the exit code as an int
func (c ExitCode) Int() int {
	return int(c)
}

//go:generate stringer -type=Status -trimprefix=Status

// Status is the enum representing the state of a job
type Status int

// NOTE: keep this synced with jobworker.proto:JobStatus
const (
	StatusUnspecified Status = iota
	StatusNotStarted         // the job has not yet been started
	StatusRunning            // the job has been started
	StatusCompleted          // the job completed successfully on its own
	StatusStopped            // the job completed after being manually signaled to stop
)

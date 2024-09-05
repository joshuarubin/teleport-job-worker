package job

type ExitCode int

func (c ExitCode) Int() int {
	return int(c)
}

//go:generate stringer -type=Status -trimprefix=Status
type Status int

// NOTE: keep this synced with jobworker.proto:JobStatus
const (
	StatusUnspecified Status = iota
	StatusNotStarted         // the job has not yet been started
	StatusRunning            // the job has been started
	StatusComplete           // the job completed successfully on its own
	StatusStopped            // the job completed after being manually signaled to stop
)

type StatusResponse struct {
	Status Status

	// ExitCode is optional since, in the case the job is still running or was
	// killed, it may not have one
	ExitCode *ExitCode

	// Error is optional and should only be checked for complete or stopped
	// jobs. Sometimes a job may complete with an error that does not have an
	// exit code, so this may be able to provide more insight.
	Error error
}

package worker

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"

	"github.com/joshuarubin/teleport-job-worker/pkg/job"
)

// ReexecCommand contains the necessary configuration for the JobWorker to be
// able to call exec.Command() and expect that the current binary will be
// reexecuted, the JobWorker will be reinstantiated, and
// JobWorker.StartJobChild() will be called with the remaining arguments passed
// in as command and args
type Config struct {
	ReexecCommand string   // often "/proc/self/exe"
	ReexecArgs    []string // usually a command that puts this binary in a different "mode", e.g. "child" or "helper"
	ReexecEnv     []string // additional env variables, in the form of "key=value", to be added to the current os.Environ()
	CPUMax        string   // the value for cpu.max in the cgroup
	MemoryMax     string   // the value for memory.max in the cgroup
	IOMax         []string // the values for io.max in the cgroup, per-device
}

// copy returns a deep copy of Config
func (c *Config) copy() *Config {
	ret := Config{
		ReexecCommand: c.ReexecCommand,
		CPUMax:        c.CPUMax,
		MemoryMax:     c.MemoryMax,
	}

	ret.ReexecArgs = make([]string, len(c.ReexecArgs))
	copy(ret.ReexecArgs, c.ReexecArgs)

	ret.ReexecEnv = make([]string, len(c.ReexecEnv))
	copy(ret.ReexecEnv, c.ReexecEnv)

	ret.IOMax = make([]string, len(c.IOMax))
	copy(ret.IOMax, c.IOMax)

	return &ret
}

// Worker is an implementation of the Worker interface
type Worker struct {
	cfg *Config

	createRootCGroupOnce sync.Once
	rootCGroupCreateErr  error
	rootCGroupName       string

	mu   sync.RWMutex
	jobs map[job.ID]*job.Job
}

// New creates a new JobWorker
func New(config *Config) *Worker {
	return &Worker{
		cfg:  config.copy(),
		jobs: map[job.ID]*job.Job{},
	}
}

// StartJob executes command, with optional args, in a new pid, mount and
// network namespace. It also creates a new cgroup and applies cpu.max,
// memory.max and io.max limits. The userID is an opaque value that is used for
// authorization of later requests. Only matching userIDs will be able to Stop
// or get the Status or Output of a job. Returns the opaque job.ID that is
// required for subsequent operations with the job.
func (w *Worker) StartJob(userID job.UserID, command string, args ...string) (job.ID, error) {
	cmdArgs := append(w.cfg.ReexecArgs, command) //nolint:gocritic
	cmdArgs = append(cmdArgs, args...)

	j, err := job.New(
		userID,
		w.cfg.ReexecCommand,
		cmdArgs,
		w.cfg.ReexecEnv,
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

// cgroupFilePerm is the file permission that is used when creating the files
// inside the cgroup
const cgroupFilePerm = 0o400

// createRootCGroup creates the root cgroup. this is only done once. sets the
// proper values on cgroup.subtree_control so that cpu, memory and io can be
// managed on leaf cgroups.
func (w *Worker) createRootCGroup() {
	// Requires cgroup v2.
	const prefix = "/sys/fs/cgroup"

	cg, err := os.MkdirTemp(prefix, "job-worker-")
	if err != nil {
		w.rootCGroupCreateErr = fmt.Errorf("error creating root cgroup: %w", err)
		return
	}

	w.rootCGroupName = cg

	err = os.WriteFile(filepath.Join(cg, "cgroup.subtree_control"), []byte("+cpu +memory +io"), cgroupFilePerm)
	if err != nil {
		w.rootCGroupCreateErr = fmt.Errorf("error writing cgroup.subtree_control: %w", err)
	}
}

// createCGroup creates a cgroup and sets the values for cpu.max, memory.max
// and io.max. it also adds the current process's pid to cgroup.procs.
func (w *Worker) createCGroup() error {
	subCGroup, err := os.MkdirTemp(w.rootCGroupName, "job-")
	if err != nil {
		return err
	}

	type cgroupValue struct {
		file  string
		value string
	}

	cgroupData := []cgroupValue{{
		file: "cgroup.procs", value: strconv.Itoa(os.Getpid()),
	}}

	if v := w.cfg.CPUMax; v != "" {
		cgroupData = append(cgroupData, cgroupValue{
			file: "cpu.max", value: v,
		})
	}

	if v := w.cfg.MemoryMax; v != "" {
		cgroupData = append(cgroupData, cgroupValue{
			file: "memory.max", value: v,
		})
	}

	for _, v := range w.cfg.IOMax {
		if v != "" {
			cgroupData = append(cgroupData, cgroupValue{
				file: "io.max", value: v,
			})
		}
	}

	for _, v := range cgroupData {
		file := filepath.Join(subCGroup, v.file)
		if err = os.WriteFile(file, []byte(v.value), cgroupFilePerm); err != nil {
			return fmt.Errorf("error writing to cgroup file %q: %w", file, err)
		}
	}

	return nil
}

// linuxOS is the value expected by runtime.GOOS on linux
const linuxOS = "linux"

// StartJobChild is called when this binary is reexecuted with the new
// namespaces applied. It should be the only method called by the binary in that
// circumstance and should never be called in any other situation. It will
// create a new cgroup with cpu, memory and io limits applied, then it will
// remount /proc and finally it will execute the command, with optional args.
func (w *Worker) StartJobChild(command string, args ...string) error {
	cmd, err := exec.LookPath(command)
	if err != nil {
		err = fmt.Errorf("lookpath error: %w", err)
		slog.Error("error starting child process", "err", err)
		return err
	}

	if runtime.GOOS == linuxOS {
		w.createRootCGroupOnce.Do(w.createRootCGroup)
		if w.rootCGroupCreateErr != nil {
			err = fmt.Errorf("error creating root cgroup: %w", w.rootCGroupCreateErr)
			slog.Error("error starting child process", "err", err)
			return err
		}

		if err = w.createCGroup(); err != nil {
			err = fmt.Errorf("error creating cgroup: %w", err)
			slog.Error("error starting child process", "err", err)
			return err
		}

		if err = mountProc(); err != nil {
			err = fmt.Errorf("error mounting /proc: %w", err)
			slog.Error("error starting child process", "err", err)
			return err
		}
	}

	args = append([]string{cmd}, args...)
	if err = syscall.Exec(cmd, args, os.Environ()); err != nil {
		err = fmt.Errorf("syscall.Exec error: %w", err)
		slog.Error("error starting child process", "err", err)
		return err
	}

	return nil
}

// ErrJobNotFound is returned when trying to stop, get status or get output of a
// job that doesn't exist or that the user is not authorized for.
var ErrJobNotFound = errors.New("job not found")

func (w *Worker) getJob(userID job.UserID, jobID job.ID) (*job.Job, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	j, ok := w.jobs[jobID]
	if !ok || j.UserID() != userID {
		return nil, ErrJobNotFound
	}

	return j, nil
}

// StopJob kills the job identified by jobID. If the job does not exist, or if
// the user is not authorized, ErrJobNotFound will be returned.
func (w *Worker) StopJob(userID job.UserID, jobID job.ID) error {
	j, err := w.getJob(userID, jobID)
	if err != nil {
		return err
	}

	return j.Stop()
}

// StatusResponse is returned by Worker.JobStatus to group the status, exit
// code and error that may be returned from a job
type StatusResponse struct {
	Status job.Status

	// ExitCode is optional since, in the case the job is still running or was
	// killed, it may not have one
	ExitCode *job.ExitCode

	// Error is optional and should only be checked for complete or stopped
	// jobs. Sometimes a job may complete with an error that does not have an
	// exit code, so this may be able to provide more insight.
	Error error
}

// JobStatus will return the JobStatus and optional ExitCode from the job. The
// ExitCode will not exist if the job is still running. If the job does not
// exist, or if the user is not authorized, ErrJobNotFound will be returned.
func (w *Worker) JobStatus(userID job.UserID, jobID job.ID) (*StatusResponse, error) {
	j, err := w.getJob(userID, jobID)
	if err != nil {
		return nil, err
	}

	return &StatusResponse{
		Status:   j.Status(),
		Error:    j.Error(),
		ExitCode: j.ExitCode(),
	}, nil
}

// JobOutput returns an io.ReadCloser that can be used to stream the output of a
// job. Creating multiple readers for a single job is safe. It is the
// responsibility of the caller to close the reader when done to free resources.
// If the job does not exist, or if the user is not authorized, ErrJobNotFound
// will be returned.
func (w *Worker) JobOutput(userID job.UserID, jobID job.ID) (io.ReadCloser, error) {
	j, err := w.getJob(userID, jobID)
	if err != nil {
		return nil, err
	}
	return j.NewOutputReader(), nil
}

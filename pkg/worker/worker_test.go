package worker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joshuarubin/teleport-job-worker/pkg/job"
	"github.com/joshuarubin/teleport-job-worker/pkg/safebuffer/safereader"
)

func TestMain(m *testing.M) {
	switch os.Getenv("GO_TEST_MODE") {
	case "":
		// Normal test mode
		os.Exit(m.Run())
	case "child":
		if len(os.Args) < 2 {
			fmt.Fprintf(os.Stderr, "command is required")
			os.Exit(1)
		}
		child(os.Args[1], os.Args[2:]...)
	}
}

func newJobWorker() (*Worker, error) {
	memoryMax := uint32(134217728)
	if v := os.Getenv("GO_TEST_JOB_WORKER_MEMORY_MAX"); v != "" {
		m, err := strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
		memoryMax = uint32(m)
	}

	cfg := Config{
		CPUMax:        .25,
		MemoryMax:     memoryMax,
		RIOPSMax:      100,
		WIOPSMax:      10,
		ReexecCommand: os.Args[0], // /proc/self/exe doesn't work on mac
		ReexecEnv:     []string{"GO_TEST_MODE=child"},
	}
	return New(&cfg)
}

func child(command string, args ...string) {
	w, err := newJobWorker()
	if err != nil {
		panic(err)
	}
	err = w.StartJobChild(command, args...)
	if err != nil {
		panic(err)
	}
}

func TestJobWorker(t *testing.T) {
	t.Parallel()

	t.Run("long-running-job", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)
		assert := assert.New(t)

		userID := job.UserID("userID")
		w, err := newJobWorker()
		require.NoError(err)

		jobID, err := w.StartJob(userID, "sh", "-c", "while true; do echo y && sleep .1; done")
		require.NoError(err)

		st, err := w.JobStatus(userID, jobID)
		require.NoError(err)
		assert.Equal(job.StatusRunning, st.Status)

		badUserID := job.UserID("foo")
		_, err = w.JobStatus(badUserID, jobID)
		require.ErrorIs(ErrJobNotFound, err)

		r, err := w.JobOutput(userID, jobID)
		require.NoError(err)

		p := make([]byte, 2)
		var n int
		for range 10 {
			n, err = r.Read(p)
			require.NoError(err)
			assert.Equal(2, n)
			assert.Equal("y\n", string(p))
		}

		err = r.Close()
		require.NoError(err)

		_, err = io.ReadAll(r)
		require.ErrorIs(err, safereader.ErrReaderClosed)

		err = w.StopJob(userID, jobID)
		require.NoError(err)

		st, err = w.JobStatus(userID, jobID)
		require.NoError(err)
		assert.Equal(job.StatusStopped, st.Status)
		require.NotNil(st.ExitCode)
		assert.Equal(-1, st.ExitCode.Int())
		assert.Error(st.Error)
	})

	t.Run("short-job", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)
		assert := assert.New(t)

		userID := job.UserID("userID")
		w, err := newJobWorker()
		require.NoError(err)

		jobID, err := w.StartJob(userID, "sh", "-c", "true")
		require.NoError(err)

		r, err := w.JobOutput(userID, jobID)
		require.NoError(err)

		_, err = io.ReadAll(r)
		require.NoError(err)

		st, err := w.JobStatus(userID, jobID)
		require.NoError(err)
		assert.Equal(job.StatusCompleted, st.Status)
	})

	t.Run("multiple-readers", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)

		userID := job.UserID("userID")
		w, err := newJobWorker()
		require.NoError(err)

		jobID, err := w.StartJob(userID, "sh", "-c", "while true; do echo y && sleep .1; done")
		require.NoError(err)

		r0, err := w.JobOutput(userID, jobID)
		require.NoError(err)
		r1, err := w.JobOutput(userID, jobID)
		require.NoError(err)

		errCh0 := make(chan error)
		errCh1 := make(chan error)

		go func() {
			defer close(errCh0)
			if _, err0 := io.ReadAll(r0); err0 != nil {
				errCh0 <- err0
				return
			}
		}()

		go func() {
			defer close(errCh1)
			if _, err1 := io.ReadAll(r1); err1 != nil {
				errCh1 <- err1
				return
			}
		}()

		time.Sleep(1 * time.Second)

		err = w.StopJob(userID, jobID)
		require.NoError(err)

		err = <-errCh0
		require.NoError(err)
		err = <-errCh1
		require.NoError(err)
	})

	t.Run("namespace", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS != linuxOS {
			t.Skip()
		}

		require := require.New(t)
		assert := assert.New(t)

		userID := job.UserID("userID")
		w, err := newJobWorker()
		require.NoError(err)

		jobID, err := w.StartJob(userID, "sh", "-c", "echo $$")
		require.NoError(err)

		r, err := w.JobOutput(userID, jobID)
		require.NoError(err)

		data, err := io.ReadAll(r)
		require.NoError(err)
		data = bytes.TrimSpace(data)
		assert.Equal("1", string(data))
	})

	t.Run("cgroup", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS != linuxOS {
			t.Skip()
		}

		require := require.New(t)
		assert := assert.New(t)

		userID := job.UserID("userID")
		w, err := newJobWorker()
		require.NoError(err)
		w.cfg.ReexecEnv = append(w.cfg.ReexecEnv,
			// anything should need more than 1B of memory, right?
			"GO_TEST_JOB_WORKER_MEMORY_MAX=1",
		)

		jobID, err := w.StartJob(userID, "yes")
		require.NoError(err)

		r, err := w.JobOutput(userID, jobID)
		require.NoError(err)

		data, err := io.ReadAll(r)
		require.NoError(err)
		assert.Empty(data)

		st, err := w.JobStatus(userID, jobID)
		require.NoError(err)
		assert.Equal(job.StatusCompleted, st.Status)
		require.NotNil(st.ExitCode)
		assert.Equal(-1, st.ExitCode.Int())
		assert.Error(st.Error)
	})
}

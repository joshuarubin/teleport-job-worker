package jobworker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joshuarubin/teleport-job-worker/internal/config"
	"github.com/joshuarubin/teleport-job-worker/internal/job"
	"github.com/joshuarubin/teleport-job-worker/internal/user"
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

func newJobWorker() *JobWorker {
	memoryMax := os.Getenv("GO_TEST_JOB_WORKER_MEMORY_MAX")
	if memoryMax == "" {
		memoryMax = "128M"
	}

	cfg := config.Config{
		CPUMax:    "25000 100000",
		MemoryMax: memoryMax,
	}
	r := ReexecCommand{
		Command: os.Args[0], // /proc/self/exe doesn't work on mac
		Env:     []string{"GO_TEST_MODE=child"},
	}
	return New(&cfg, &r)
}

func child(command string, args ...string) {
	_ = newJobWorker().
		StartJobChild(context.Background(), command, args...)
}

func TestJobWorker(t *testing.T) {
	t.Parallel()

	t.Run("long-running-job", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)
		assert := assert.New(t)

		ctx := context.Background()
		userID := user.ID("userID")
		w := newJobWorker()

		jobID, err := w.StartJob(ctx, userID, "sh", "-c", "while true; do echo y && sleep .1; done")
		require.NoError(err)

		st, err := w.JobStatus(ctx, userID, jobID)
		require.NoError(err)
		assert.Equal(job.StatusRunning, st.Status)

		badUserID := user.ID("foo")
		_, err = w.JobStatus(ctx, badUserID, jobID)
		require.ErrorIs(ErrJobNotFound, err)

		r, err := w.JobOutput(ctx, userID, jobID)
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
		require.NoError(err)

		err = w.StopJob(ctx, userID, jobID)
		require.NoError(err)

		st, err = w.JobStatus(ctx, userID, jobID)
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

		ctx := context.Background()
		userID := user.ID("userID")
		w := newJobWorker()

		jobID, err := w.StartJob(ctx, userID, "sh", "-c", "true")
		require.NoError(err)

		r, err := w.JobOutput(ctx, userID, jobID)
		require.NoError(err)

		_, err = io.ReadAll(r)
		require.NoError(err)

		st, err := w.JobStatus(ctx, userID, jobID)
		require.NoError(err)
		assert.Equal(job.StatusComplete, st.Status)
	})

	t.Run("multiple-readers", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)

		ctx := context.Background()
		userID := user.ID("userID")
		w := newJobWorker()

		jobID, err := w.StartJob(ctx, userID, "sh", "-c", "while true; do echo y && sleep .1; done")
		require.NoError(err)

		r0, err := w.JobOutput(ctx, userID, jobID)
		require.NoError(err)
		r1, err := w.JobOutput(ctx, userID, jobID)
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

		err = w.StopJob(ctx, userID, jobID)
		require.NoError(err)

		err = <-errCh0
		require.NoError(err)
		err = <-errCh1
		require.NoError(err)
	})

	t.Run("namespace", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS != "linux" {
			t.Skip()
		}

		require := require.New(t)
		assert := assert.New(t)

		ctx := context.Background()
		userID := user.ID("userID")
		w := newJobWorker()

		jobID, err := w.StartJob(ctx, userID, "sh", "-c", "echo $$")
		require.NoError(err)

		r, err := w.JobOutput(ctx, userID, jobID)
		require.NoError(err)

		data, err := io.ReadAll(r)
		require.NoError(err)
		data = bytes.TrimSpace(data)
		assert.Equal("1", string(data))
	})

	t.Run("cgroup", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS != "linux" {
			t.Skip()
		}

		require := require.New(t)
		assert := assert.New(t)

		ctx := context.Background()
		userID := user.ID("userID")
		w := newJobWorker()
		w.reexec.Env = append(w.reexec.Env,
			// anything should need more than 1B of memory, right?
			"GO_TEST_JOB_WORKER_MEMORY_MAX=1",
		)

		jobID, err := w.StartJob(ctx, userID, "yes")
		require.NoError(err)

		r, err := w.JobOutput(ctx, userID, jobID)
		require.NoError(err)

		data, err := io.ReadAll(r)
		require.NoError(err)
		assert.Empty(data)

		st, err := w.JobStatus(ctx, userID, jobID)
		require.NoError(err)
		assert.Equal(job.StatusComplete, st.Status)
		require.NotNil(st.ExitCode)
		assert.Equal(-1, st.ExitCode.Int())
		assert.Error(st.Error)
	})
}

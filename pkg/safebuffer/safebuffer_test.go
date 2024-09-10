package safebuffer

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bufWrite(buf *Buffer, v string) <-chan error {
	ch := make(chan error)
	go func() {
		_, err := buf.Write([]byte(v))
		ch <- err
	}()
	return ch
}

func TestSafeBuffer(t *testing.T) {
	t.Parallel()

	t.Run("sync", func(t *testing.T) {
		t.Parallel()

		// this test is checking to make sure that the reader blocks until the
		// job completes

		assert := assert.New(t)
		require := require.New(t)

		jobDone := make(chan struct{})
		buf := New(jobDone)

		// 1. Write to the buffer
		var err error
		for range 3 {
			if e := <-bufWrite(buf, "foo"); e != nil {
				err = e
				break
			}
		}

		require.NoError(err)
		assert.Equal("foofoofoo", buf.buf.String())

		// 2. Synchronous read everything from the buffer
		r0 := buf.NewReader()
		b := make([]byte, 3)
		var n int
		for range 3 {
			n, err = r0.Read(b)
			require.NoError(err)
			assert.Equal(3, n)
			assert.Equal("foo", string(b[:n]))
		}

		// 3. Try reading more from the buffer in the goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			for err != io.EOF {
				n, err = r0.Read(b)
			}
		}()

		// 4. This should time out because nothing more is written
		timer := time.NewTimer(10 * time.Nanosecond)
		select {
		case <-timer.C:
		case <-done:
			t.Fatal("expected Read not to complete because jobDone wasn't closed")
		}

		// 5. Close the jobDone channel to simulate the job completing
		close(jobDone)

		// 6. Now the reader should return io.EOF and the done channel
		// will close
		<-done
		assert.Equal(io.EOF, err)
		assert.Equal(0, n)
	})

	t.Run("multiple-readers", func(t *testing.T) {
		t.Parallel()
		assert := assert.New(t)
		require := require.New(t)

		jobDone := make(chan struct{})
		buf := New(jobDone)

		var err error
		for range 3 {
			if e := <-bufWrite(buf, "foo"); e != nil {
				err = e
				break
			}
		}

		require.NoError(err)
		assert.Equal("foofoofoo", buf.buf.String())

		close(jobDone)

		r0 := buf.NewReader()
		r1 := buf.NewReader()

		for _, r := range []io.Reader{r0, r1} {
			data, err := io.ReadAll(r)
			require.NoError(err)
			assert.Equal("foofoofoo", string(data))
		}
	})

	t.Run("write-after-initial-read-complete", func(t *testing.T) {
		t.Parallel()
		assert := assert.New(t)
		require := require.New(t)

		jobDone := make(chan struct{})
		buf := New(jobDone)

		var err error
		for range 3 {
			if e := <-bufWrite(buf, "foo"); e != nil {
				err = e
				break
			}
		}

		require.NoError(err)
		assert.Equal("foofoofoo", buf.buf.String())

		r := buf.NewReader()
		b := make([]byte, 9)
		n, err := r.Read(b)
		assert.Equal(9, n)
		require.NoError(err)
		assert.Equal("foofoofoo", string(b))

		err = <-bufWrite(buf, "bar")
		require.NoError(err)

		n, err = r.Read(b)
		assert.Equal(3, n)
		require.NoError(err)
		assert.Equal("bar", string(b[:n]))
	})

	t.Run("concurrency", func(t *testing.T) {
		t.Parallel()
		assert := assert.New(t)
		require := require.New(t)

		jobDone := make(chan struct{})
		buf := New(jobDone)

		times := 1000
		msg := "foo"

		errCh0 := make(chan error)
		go func() {
			defer close(errCh0)
			defer close(jobDone)

			for range times {
				if err := <-bufWrite(buf, msg); err != nil {
					errCh0 <- err
					return
				}
			}
		}()

		errCh1 := make(chan error)
		read := 0
		go func() {
			defer close(errCh1)
			r := buf.NewReader()
			b := make([]byte, 16)
			for {
				n, err := r.Read(b)
				if err == io.EOF {
					return
				}
				if err != nil {
					errCh1 <- err
					return
				}
				read += n
			}
		}()

		err, ok := <-errCh0
		require.NoError(err)
		assert.False(ok)

		err, ok = <-errCh1
		require.NoError(err)
		assert.False(ok)
		assert.Equal(times*len(msg), read)
	})
}

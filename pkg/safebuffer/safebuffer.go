package safebuffer

import (
	"bytes"
	"io"
	"sync"

	"github.com/joshuarubin/teleport-job-worker/pkg/safebuffer/safereader"
)

// Buffer is a goroutine safe buffer that ingests data as an io.Writer and
// provides io.ReadClosers to reply and continue to stream incoming data.
type Buffer struct {
	mu      sync.RWMutex
	buf     bytes.Buffer
	jobDone <-chan struct{}
	chans   map[chan []byte]<-chan struct{}
}

// ensure safeBuffer implements the io.Writer interface
var _ io.Writer = (*Buffer)(nil)

// New creates a new Buffer. Readers created with NewReader will be
// automatically closed when the context becomes done or the jobDone channel
// closes.
func New(jobDone <-chan struct{}) *Buffer {
	b := Buffer{
		jobDone: jobDone,
		chans:   map[chan []byte]<-chan struct{}{},
	}
	go b.jobWatcher()
	return &b
}

// jobWatcher waits for jobDone to close at which point it will close all reader
// data channels and remove them from the buffer's internal map.
func (b *Buffer) jobWatcher() {
	<-b.jobDone

	b.mu.Lock()
	defer b.mu.Unlock()

	// it should not be possible for any more writes to occur after jobDone is
	// closed and the mutex is acquired.
	//
	// from exec godocs: https://pkg.go.dev/os/exec#Cmd.Wait
	// Wait() waits for the command to exit and waits for any copying to stdin
	// or copying from stdout or stderr to complete.
	//
	// The jobDone channel, necessarily, can only be closed after Wait()
	// completes. This means that all Write() calls to the SafeBuffer, which are
	// synchronous, must complete before dataCh can be closed. The SafeReader
	// may receive data via channel in a separate goroutine, which, once
	// received unblocks the Write() call in the SafeBuffer. However, Write()
	// holds the mutex for the entire time it executes. To get to this point,
	// jobWatcher() must hold that mutex. This means that all of the channels
	// will receive all of their data before close() is called on them and so
	// all of the data will make it through the channel.

	for dataCh := range b.chans {
		close(dataCh)
		delete(b.chans, dataCh)
	}
}

// Write is the io.Writer interface that keeps the source of truth for the job
// output. It also sends new data to any connected readers.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for dataCh, readerClosed := range b.chans {
		select {
		case <-readerClosed:
			// reader is closed, remove it from the map
			close(dataCh)
			delete(b.chans, dataCh)
		default:
			v := make([]byte, len(p))
			copy(v, p)

			// The reader's receiver runs in its own goroutine where it writes
			// to its own buffer. It does need to take a lock when writing so
			// there is no contention while the reader's buffer is being read by
			// clients. It holds the lock for the shortest period possible while
			// reading and writing to the reader buffer. While there may be a
			// very brief stutter, it is never be dependent on
			// external/uncontrollable factors (like the client reading the
			// io.ReadCloser) as to how quickly it can be consumed. As such,
			// there is no direct concern regarding the speed at which clients
			// consume data from the reader causing sends to this channel to
			// block
			dataCh <- v
		}
	}

	return b.buf.Write(p)
}

// NewReader creates a new io.ReadCloser that can be used to stream the output
// of the job. It is the caller's responsibility to close the reader when done.
func (b *Buffer) NewReader() io.ReadCloser {
	b.mu.Lock()
	defer b.mu.Unlock()

	r, ch := safereader.New(b.buf.Bytes(), b.jobDone)
	if ch != nil {
		b.chans[ch.Data] = ch.Closed
	}

	return r
}

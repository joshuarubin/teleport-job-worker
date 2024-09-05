package safebuffer

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/joshuarubin/teleport-job-worker/internal/safebuffer/safereader"
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
func New(ctx context.Context, jobDone <-chan struct{}) *Buffer {
	b := Buffer{
		jobDone: jobDone,
		chans:   map[chan []byte]<-chan struct{}{},
	}
	go b.jobWatcher(ctx)
	return &b
}

// jobWatcher waits for the context to become done or for jobDone to close at
// which point it will close all reader data channels and remove them from the
// buffer's internal map.
func (b *Buffer) jobWatcher(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-b.jobDone:
	}

	b.mu.Lock()
	defer b.mu.Unlock()

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

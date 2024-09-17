package safereader

import (
	"context"
	"errors"
	"io"
	"sync"
)

// Reader is a goroutine safe io.ReadCloser that streams Buffer data from the
// beginning
type Reader struct {
	Buffer

	cancel func()
	closed func() <-chan struct{}

	mu     sync.RWMutex
	offset int
	wake   chan struct{}
}

// jobIsDone returns true if the job has completed
func (r *Reader) jobIsDone() bool {
	select {
	case <-r.Done():
		return true
	default:
		return false
	}
}

// IsClosed returns true if the reader has been closed
func (r *Reader) IsClosed() bool {
	select {
	case <-r.closed():
		return true
	default:
		return false
	}
}

// Await returns a channel that blocks until the reader has been woken with
// Wake()
func (r *Reader) Await() <-chan struct{} {
	var ch chan struct{}
	r.mu.RLock()
	ch = r.wake
	r.mu.RUnlock()
	return ch
}

// Wake is called after the Buffer has written new data
func (r *Reader) Wake() {
	r.mu.Lock()
	close(r.wake)
	r.wake = make(chan struct{})
	r.mu.Unlock()
}

// Buffer is used to prevent an import cycle
type Buffer interface {
	ReadOffset(offset int, p []byte) (int, error)
	Done() <-chan struct{}
}

// New returns a new Reader that will read from the beginning of Buffer until
// io.EOF is returned after Done() closes.
func New(b Buffer) *Reader {
	ctx, cancel := context.WithCancel(context.Background())

	return &Reader{
		cancel: cancel,
		closed: ctx.Done,
		wake:   make(chan struct{}),
		Buffer: b,
	}
}

// readOffset safely reads from the buffer into p and updates the offset by the
// number of bytes read
func (r *Reader) readOffset(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, err := r.ReadOffset(r.offset, p)
	if err == nil {
		r.offset += n
	}

	return n, err
}

// ErrReaderClosed is returned by Read() after the Reader.Close() is called
var ErrReaderClosed = errors.New("reader is closed")

// Read is the io.Reader interface and returns up to len(p) data in p. The
// number of bytes written is returned.
func (r *Reader) Read(p []byte) (int, error) {
	if r.IsClosed() {
		return 0, ErrReaderClosed
	}

	for {
		n, err := r.readOffset(p)
		if !errors.Is(err, io.EOF) || r.jobIsDone() {
			return n, err
		}

		// got io.EOF and job isn't done yet
		select {
		case <-r.Await():
		case <-r.closed():
			return n, ErrReaderClosed
		case <-r.Done():
			return n, io.EOF
		}
	}
}

// Close is the io.Closer interface and causes the Buffer to remove the Reader
// from its resources. Any Reads after being closed will return
// ErrReaderClosed.
func (r *Reader) Close() error {
	r.cancel()
	return nil
}

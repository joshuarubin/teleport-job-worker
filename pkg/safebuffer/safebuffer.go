package safebuffer

import (
	"bytes"
	"io"
	"sync"

	"github.com/joshuarubin/teleport-job-worker/pkg/safebuffer/safereader"
)

// Buffer is a goroutine safe buffer that ingests data as an io.Writer and
// provides io.ReadClosers to replay and continue to stream incoming data.
type Buffer struct {
	Readers

	bufMu sync.RWMutex
	buf   bytes.Buffer

	done <-chan struct{}
}

// ensure Buffer implements the io.Writer interface
var _ io.Writer = (*Buffer)(nil)

// New creates a new Buffer
func New(done <-chan struct{}) *Buffer {
	return &Buffer{done: done}
}

// Write is the io.Writer interface that writes to the buffer and notifies
// readers that more data is available
func (b *Buffer) Write(p []byte) (int, error) {
	b.bufMu.Lock()
	n, werr := b.buf.Write(p)
	b.bufMu.Unlock()

	for it := b.Iterator(); it.Next(); {
		reader := it.Reader()

		if reader.IsClosed() {
			it.Delete()
			continue
		}

		reader.Wake()
	}

	return n, werr
}

// ReadOffset is called by readers to read from a given offset into p
func (b *Buffer) ReadOffset(offset int, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.bufMu.RLock()
	defer b.bufMu.RUnlock()

	data := b.buf.Bytes()

	if len(data) <= offset {
		return 0, io.EOF
	}

	n := copy(p, data[offset:])
	return n, nil
}

// Done returns a channel that's closed when the done channel passed into New()
// closes
func (b *Buffer) Done() <-chan struct{} {
	return b.done
}

// NewReader creates a new io.ReadCloser that can be used to stream the output
// of the job. It is the caller's responsibility to close the reader when done.
func (b *Buffer) NewReader() io.ReadCloser {
	r := safereader.New(b)
	b.Add(r)
	return r
}

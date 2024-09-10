package safebuffer

import (
	"io"
	"sync"
)

// ByteBuffer implements a goroutine safe buffer
type ByteBuffer struct {
	mu  sync.RWMutex
	buf []byte
}

// String returns the buffered data as a string
func (b *ByteBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return string(b.buf)
}

// ReadOffset is called by readers to read from a given offset into p
func (b *ByteBuffer) ReadOffset(offset int, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.buf) <= offset {
		return 0, io.EOF
	}

	return copy(p, b.buf[offset:]), nil
}

// minBufferSize is the minimum size buffer a new ByteBuffer will use
const minBufferSize = 64

// Write is the io.Writer interface that writes to the buffer
func (b *ByteBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buf == nil {
		b.buf = make([]byte, 0, max(minBufferSize, len(p)*2)) //nolint:mnd
	}

	n := len(p)
	l := len(b.buf)
	if n <= cap(b.buf)-l {
		b.buf = b.buf[:l+n]
	} else {
		c := max(len(b.buf)+n, 2*cap(b.buf)) //nolint:mnd
		newBuf := make([]byte, l+n, c)
		copy(newBuf, b.buf)
		b.buf = newBuf
	}
	return copy(b.buf[l:], p), nil
}

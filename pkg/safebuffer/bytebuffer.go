package safebuffer

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

// byteNode is a linked list node
type byteNode struct {
	data []byte
	next atomic.Pointer[byteNode]
}

// ByteBuffer implements a goroutine safe buffer implemented with an immutable
// linked list. it uses a mutex when reading and modifying its own values, but
// does not need any lock once it begins walking the list.
type ByteBuffer struct {
	mu        sync.RWMutex
	root, end *byteNode
	size      int
}

// String returns the buffered data as a string
func (b *ByteBuffer) String() string {
	b.mu.RLock()
	node := b.root
	b.mu.RUnlock()

	var s strings.Builder
	for node != nil {
		s.Write(node.data)
		node = node.next.Load()
	}
	return s.String()
}

// ReadOffset is called by readers to read from a given offset into p
func (b *ByteBuffer) ReadOffset(offset int, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	b.mu.RLock()
	if b.size <= offset {
		b.mu.RUnlock()
		return 0, io.EOF
	}
	node := b.root
	b.mu.RUnlock()

	var n int
	for node != nil {
		if offset >= len(node.data) {
			offset -= len(node.data)
			node = node.next.Load()
			continue
		}

		i := copy(p, node.data[offset:])
		n += i
		if i == len(p) {
			break
		}
		p = p[i:]
		offset = 0
		node = node.next.Load()
	}

	if n > 0 {
		return n, nil
	}

	return 0, io.EOF
}

// Write is the io.Writer interface that writes to the buffer
func (b *ByteBuffer) Write(p []byte) (int, error) {
	node := byteNode{
		data: make([]byte, len(p)),
	}
	n := copy(node.data, p)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.root == nil {
		b.root = &node
		b.end = &node
		b.size = len(node.data)
		return n, nil
	}

	b.end.next.Store(&node)
	b.end = &node
	b.size += len(node.data)

	return n, nil
}

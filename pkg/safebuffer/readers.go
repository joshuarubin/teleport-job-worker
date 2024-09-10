package safebuffer

import (
	"sync"

	"github.com/joshuarubin/teleport-job-worker/pkg/safebuffer/safereader"
)

// Readers manages a list of safereader.Readers
type Readers struct {
	mu   sync.RWMutex
	list []*safereader.Reader
}

// Add a Reader to the list
func (c *Readers) Add(chans *safereader.Reader) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, chans)
}

// ReadersIterator is an iterator used for looping throuth the list
type ReadersIterator struct {
	pos int
	*Readers
}

// Iterator returns an iterator
func (c *Readers) Iterator() *ReadersIterator {
	return &ReadersIterator{
		Readers: c,
		pos:     -1,
	}
}

// Next returns true if there is an available reader. It should be called
// before any other iterator method.
func (i *ReadersIterator) Next() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.nextNoLock()
}

func (i *ReadersIterator) nextNoLock() bool {
	for i.pos+1 < len(i.list) {
		i.pos++
		if i.list[i.pos].IsClosed() {
			i.deleteNoLock()
			continue
		}
		return true
	}

	return false
}

// Reader returns the current reader
func (i *ReadersIterator) Reader() *safereader.Reader {
	for {
		i.mu.RLock()
		if i.pos >= len(i.list) {
			i.mu.RUnlock()
			return nil
		}
		r := i.list[i.pos]
		i.mu.RUnlock()

		if !r.IsClosed() {
			return r
		}
		i.Delete()
	}
}

// Delete the current iterator from the list
func (i *ReadersIterator) Delete() {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.deleteNoLock()
}

func (i *ReadersIterator) deleteNoLock() {
	if i.pos >= len(i.list) {
		return
	}

	i.list = append(i.list[:i.pos], i.list[i.pos+1:]...)
	i.pos--
}

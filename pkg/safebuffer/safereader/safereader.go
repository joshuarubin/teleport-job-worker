package safereader

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"sync"
)

// Reader is a goroutine safe io.ReadCloser that makes initial data available to
// callers as well as new data that arrives later via channel.
type Reader struct {
	mu        sync.Mutex
	buf       *bytes.Buffer
	close     func()
	closed    <-chan struct{}
	dataCh    <-chan []byte
	dataAvail chan struct{}
	closeOnce sync.Once
}

// jobIsRunning doesn't have a race because Buffer.NewReader calls it while
// holding a lock that prevents new data, or the close of jobDone, from being
// processed.
func jobIsRunning(jobDone <-chan struct{}) bool {
	select {
	case <-jobDone:
		return false
	default:
		return true
	}
}

type Channels struct {
	Data   chan []byte
	Closed <-chan struct{}
}

// New returns a new Reader that keeps a copy of data and will append any data
// that arrives on the returned Data channel. If the job is no longer running
// when created, it is treated as a closed reader and no new data can be added.
// Calls to Close() in this case are ignored.
func New(data []byte, jobDone <-chan struct{}) (*Reader, *Channels) {
	closed := make(chan struct{})
	closeFn := func() { close(closed) }

	// 	This is by far the biggest compromise I've made in the design. Keeping
	// 	a full copy of the output buffer from the time that a reader is created
	// 	is a very naive approach that is very memory and time inefficient. I
	// 	did this to keep the code simple and with the understanding the
	// 	challenge was given with "unlimited cpu and unlimited memory". This is
	// 	not a design I would ever propose for production.
	//
	// I think what I would have liked to do instead is use a RWMutex on the
	// original buffer and let the readers read directly from that and maintain
	// their own position/index. The problem I had when thinking through this
	// is that if I imagined a lot of connected readers holding RLocks, then
	// writes may block for unacceptably long times. I may be able to fix this
	// by limiting the time that RLocks are held and looping, thereby giving an
	// opportunity for the write locks to be acquired. I worried that trying to
	// figure out how to do this correctly and without races would become quite
	// complex. In keeping with the spirit of the challenge, "do the simplest
	// thing possible that satisfies the requirements", I chose what I thought
	// to be the simpler option to implement.
	//
	// There is another optimization I could also do to the current
	// implementation, which would be to discard data from readers once it has
	// been read. Again, it's more work and not necessarily a requirement, so I
	// did not choose to implement this either.

	buf := make([]byte, len(data))
	copy(buf, data)

	r := Reader{
		close:  closeFn,
		closed: closed,
		buf:    bytes.NewBuffer(buf),
	}

	var channels *Channels

	if jobIsRunning(jobDone) {
		dataCh := make(chan []byte)
		channels = &Channels{
			Data:   dataCh,
			Closed: closed,
		}
		r.dataCh = dataCh
		r.dataAvail = make(chan struct{})

		go r.receiver()
	} else {
		// The job has already completed and we know no new job output will be
		// coming in. As a result, the state of the reader is simplified in
		// that it only needs to be a buffer for the data it has right now. It
		// doesn't need to be added to the map in the SafeBuffer, it doesn't
		// need a goroutine, the calls to Read() can just defer to the internal
		// buffer. By closing it immediately, we ensure that clients won't
		// block indefinitely for jobs that have already completed and for
		// which they haven't called Close() in a separate goroutine.
		r.Close()
	}

	return &r, channels
}

func (r *Reader) isClosed() bool {
	select {
	case <-r.closed:
		return true
	default:
		return false
	}
}

// Read is the io.Reader interface and returns up to len(p) data in p. The
// number of bytes written is returned.
func (r *Reader) Read(p []byte) (int, error) {
	r.mu.Lock()

	n, err := r.buf.Read(p)
	if !errors.Is(err, io.EOF) || r.isClosed() {
		r.mu.Unlock()
		return n, err
	}

	// reading buf returned io.EOF and the job is still running

	// have to get this while the lock is held
	dataAvail := r.dataAvail

	r.mu.Unlock()

	select {
	case <-r.closed:
		// the reader was closed, so we return io.EOF
		return n, io.EOF
	case <-dataAvail:
		// more data was written to buf, so let's return that
		r.mu.Lock()
		defer r.mu.Unlock()
		var i int
		if i, err = r.buf.Read(p); errors.Is(err, io.EOF) {
			err = nil
		}
		return n + i, err
	}
}

// Close is the io.Closer interface and causes the reader to release all
// resources. It is still possible to read any remaining buffered data until
// io.EOF is received, but no new data can be added to the buffer after Close is
// called.
func (r *Reader) Close() error {
	r.closeOnce.Do(r.close)
	return nil
}

func (r *Reader) receiver() {
	for {
		select {
		case <-r.closed:
			return
		case data, ok := <-r.dataCh:
			if !ok {
				r.Close()
				return
			}
			if _, err := r.write(data); err != nil {
				slog.Error("error writing to Reader buffer", "err", err)
			}
		}
	}
}

// write implements the same function signature as io.Writer, but we don't
// want to export it and confuse callers
func (r *Reader) write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	n, err := r.buf.Write(p)

	if err == nil && n > 0 {
		close(r.dataAvail)
		r.dataAvail = make(chan struct{})
	}

	return n, err
}

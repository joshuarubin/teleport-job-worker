package safereader

import (
	"bytes"
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

var _ io.ReadCloser = (*Reader)(nil)

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
	if err != io.EOF || r.isClosed() {
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
		return 0, io.EOF
	case <-dataAvail:
		// more data was written to buf, so let's return that
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.buf.Read(p)
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

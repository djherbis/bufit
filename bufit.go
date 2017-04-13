package bufit

import (
	"container/heap"
	"io"
	"sync"
	"sync/atomic"
)

// Reader provides an io.Reader whose methods MUST be concurrent-safe
// with the Write method of the Writer from which it was generated.
// It also MUST be safe for concurrent calls to Writer.Discard
// for bytes which have already been read by this Reader.
type Reader interface {

	// Len returns the unread # of bytes in this Reader
	Len() int

	// Discard drops the next n bytes from the Reader, as if it were Read()
	// it returns the # of bytes actually dropped. It may return io.EOF
	// if all remaining bytes have been discarded.
	Discard(int) (int, error)

	// Read bytes into the provided buffer.
	io.Reader
}

// Writer accepts bytes and generates Readers who consume those bytes.
// Generated Readers methods must be concurrent-safe with the Write method.
type Writer interface {

	// Len returns the # of bytes buffered for Readers
	Len() int

	// Discard drops the next n buffered bytes. It returns the actual number of
	// bytes dropped and may return io.EOF if all remaining bytes have been
	// discarded. Discard must be concurrent-safe with methods calls
	// on generated Readers, when discarding bytes that have been read
	// by all Readers.
	Discard(int) (int, error)

	// NextReader returns a Reader which reads a "snapshot" of the current written bytes
	// (excluding discarded bytes). The Reader should work independently of the Writer
	// and be concurrent-safe with the Write method on the Writer.
	NextReader() Reader

	// Write writes the given bytes into the Writer's underlying buffer. Which will
	// be available for reading using NextReader() to grab a snapshot of the current
	// written bytes.
	io.Writer
}

// Buffer is used to provide multiple readers with access to a shared buffer.
// Readers may join/leave at any time, however a joining reader will only
// see whats currently in the buffer onwards. Data is evicted from the buffer
// once all active readers have read that section.
type Buffer struct {
	mu    sync.Mutex
	rwait *sync.Cond
	wwait *sync.Cond
	off   int
	rh    readerHeap
	buf   Writer
	cap   int
	life
	callback atomic.Value
}

type life struct {
	state int32
}

func (l *life) alive() bool { return atomic.LoadInt32(&l.state) == 0 }
func (l *life) kill()       { atomic.AddInt32(&l.state, 1) }

func (b *Buffer) fetch(r *reader) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if r.alive() {
		r.off += r.size
		r.size = 0
		heap.Fix(&b.rh, r.i)
		b.shift()
	}

	for r.off == b.off+b.buf.Len() && b.alive() && r.alive() {
		b.rwait.Wait()
	}

	if !r.alive() {
		return
	}

	r.data = b.buf.NextReader()
	r.data.Discard(r.off - b.off)
	r.size = r.data.Len()
}

func (b *Buffer) drop(r *reader) {
	b.mu.Lock()

	if len(b.rh) == 1 { // this is the last reader
		if call := b.callback.Load(); call != nil { // callback is registered
			defer call.(func() error)() // run this after we've unlocked
		}
	}

	defer b.rwait.Broadcast() // wake up and blocking reads
	defer b.mu.Unlock()
	heap.Remove(&b.rh, r.i)
	b.shift()
}

func (b *Buffer) shift() {
	if b.rh.Len() == 0 {
		return
	}

	if diff := b.rh.Peek().off - b.off; diff > 0 {
		b.buf.Discard(diff)
		b.off += diff
		b.wwait.Broadcast()
	}
}

// NumReaders returns the number of readers returned by NextReader() which have not called Reader.Close().
// This method is safe to call concurrently with all methods.
func (b *Buffer) NumReaders() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.rh)
}

// OnLastReaderClose registers the passed callback to be run after any call to Reader.Close() which drops the NumReaders() to 0.
// This method is safe to call concurrently with all other methods and Reader methods, however it's only guaranteed to be triggered if it completes before
// the Reader.Close call which would trigger it.
func (b *Buffer) OnLastReaderClose(runOnLastClose func() error) {
	b.callback.Store(runOnLastClose)
}

// NextReader returns a new io.ReadCloser for this shared buffer.
// Read/Close are safe to call concurrently with the buffers Write/Close methods.
// Read calls will block if the Buffer is not Closed and contains no data.
// Note that the returned reader sees all data that is currently in the buffer,
// data is only dropped out of the buffer once all active readers point to
// locations in the buffer after that section.
func (b *Buffer) NextReader() io.ReadCloser {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := &reader{
		buf:  b,
		size: b.buf.Len(),
		off:  b.off,
		data: b.buf.NextReader(),
	}
	heap.Push(&b.rh, r)
	return r
}

// NextReaderFromNow returns a new io.ReadCloser for this shared buffer.
// Unlike NextReader(), this reader will only see writes which occur after this reader is returned
// even if there is other data in the buffer. In other words, this reader points to the end
// of the buffer.
func (b *Buffer) NextReaderFromNow() io.ReadCloser {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.buf.Len()
	r := &reader{
		buf:  b,
		off:  b.off + l,
		data: b.buf.NextReader(),
	}
	r.data.Discard(l)
	heap.Push(&b.rh, r)
	return r
}

// Len returns the current size of the buffer. This is safe to call concurrently with all other methods.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// Write appends the given data to the buffer. All active readers will
// see this write.
func (b *Buffer) Write(p []byte) (int, error) {
	if !b.alive() {
		return 0, io.ErrClosedPipe
	}

	b.mu.Lock()
	defer b.rwait.Broadcast()
	defer b.mu.Unlock()
	if !b.alive() {
		return 0, io.ErrClosedPipe
	}

	var m, n int
	var err error
	for len(p[n:]) > 0 && err == nil { // bytes left to write

		for b.cap > 0 && b.buf.Len() == b.cap && b.alive() { // wait for space
			b.wwait.Wait()
		}

		if !b.alive() {
			return n, io.ErrClosedPipe
		}

		if b.cap == 0 || b.cap-b.buf.Len() > len(p[n:]) { // remaining bytes fit in gap, or no cap.
			m, err := b.buf.Write(p[n:])
			return n + m, err
		}

		gap := b.cap - b.buf.Len() // there is a cap, and we didn't fit in the gap
		m, err = b.buf.Write(p[n : n+gap])
		n += m
		b.rwait.Broadcast() // wake up readers to read the partial write
	}
	return n, err
}

// Close marks the buffer as complete. Readers will return io.EOF instead of blocking
// when they reach the end of the buffer.
func (b *Buffer) Close() error {
	b.mu.Lock()
	defer b.rwait.Broadcast() // readers should wake up since there will be no more writes
	defer b.wwait.Broadcast() // writers should wake up since blocking writes should unblock
	defer b.mu.Unlock()
	b.kill()
	return nil
}

// NewBuffer creates and returns a new Buffer backed by the passed Writer
func NewBuffer(w Writer) *Buffer {
	return NewCappedBuffer(w, 0)
}

// New creates and returns a new Buffer
func New() *Buffer {
	return NewBuffer(newWriter(nil))
}

// NewCapped creates a new in-memory Buffer whose Write() call blocks to prevent Len() from exceeding
// the passed capacity
func NewCapped(cap int) *Buffer {
	return NewCappedBuffer(newWriter(nil), cap)
}

// NewCappedBuffer creates a new Buffer whose Write() call blocks to prevent Len() from exceeding
// the passed capacity
func NewCappedBuffer(w Writer, cap int) *Buffer {
	buf := Buffer{
		buf: w,
		cap: cap,
	}
	buf.rwait = sync.NewCond(&buf.mu)
	buf.wwait = sync.NewCond(&buf.mu)
	return &buf
}

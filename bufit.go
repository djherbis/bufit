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
	mu   sync.RWMutex
	cond *sync.Cond
	off  int
	rh   readerHeap
	buf  Writer
	life
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
		b.cond.Wait()
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
	}
}

// NextReader returns a new ReadCloser for this shared buffer.
// Read/Close are safe to call concurrently with the buffers Write/Close methods.
// Read calls will block if the Buffer is not Closed and contains no data.
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

// Write appends the given data to the buffer. All active readers will
// see this write.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.cond.Broadcast()
	defer b.mu.Unlock()
	if !b.alive() {
		return 0, io.ErrClosedPipe
	}
	return b.buf.Write(p)
}

// Close marks the buffer as complete. Readers will return io.EOF instead of blocking
// when they reach the end of the buffer.
func (b *Buffer) Close() error {
	defer b.cond.Broadcast()
	b.kill()
	return nil
}

// NewBuffer creates and returns a new Buffer backed by the passed Writer
func NewBuffer(w Writer) *Buffer {
	buf := Buffer{
		buf: w,
	}
	buf.cond = sync.NewCond(&buf.mu)
	return &buf
}

// New creates and returns a new Buffer
func New() *Buffer {
	return NewBuffer(newWriter(nil))
}

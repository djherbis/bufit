package bufit

import (
	"container/heap"
	"io"
	"sync"
	"sync/atomic"
)

// Fixed address buffer

// Buffer is used to provide multiple readers with access to a shared buffer.
// Readers may join/leave at any time, however a joining reader will only
// see whats currently in the buffer onwards. Data is evicted from the buffer
// once all active readers have read that section.
type Buffer struct {
	mu   sync.RWMutex
	cond *sync.Cond
	off  int64
	rh   readerHeap
	data []byte
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

	for len(b.data) == 0 && b.alive() && r.alive() {
		b.cond.Wait()
	}

	if !r.alive() {
		return
	}

	r.off += int64(r.chunk)
	diff := r.off - b.off
	r.data = b.data[diff:]
	r.chunk = len(r.data)

	heap.Fix(&b.rh, r.i)
	b.shift()
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
		b.data = b.data[diff:]
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
		buf:   b,
		chunk: len(b.data),
		off:   b.off,
		data:  b.data,
	}
	heap.Push(&b.rh, r)
	return r
}

// Write appends the given data to the buffer. All active readers will
// see this write.
func (b *Buffer) Write(p []byte) (int, error) {
	data := append(b.data, p...)
	b.mu.Lock()
	defer b.cond.Broadcast()
	defer b.mu.Unlock()
	b.data = data
	return len(p), nil
}

// Close marks the buffer as complete. Readers will return io.EOF instead of blocking
// when they reach the end of the buffer.
func (b *Buffer) Close() error {
	defer b.cond.Broadcast()
	b.kill()
	return nil
}

// New creates and returns a new Buffer
func New() *Buffer {
	buf := Buffer{}
	buf.cond = sync.NewCond(&buf.mu)
	return &buf
}

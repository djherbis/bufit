package bufit

import (
	"io"
	"sync"
)

type readerHeap []*reader

func (h readerHeap) Len() int           { return len(h) }
func (h readerHeap) Less(i, j int) bool { return h[i].off < h[j].off }
func (h readerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].i = i
	h[j].i = j
}

func (h *readerHeap) Push(x interface{}) {
	r := x.(*reader)
	r.i = len(*h)
	*h = append(*h, r)
}

func (h *readerHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h readerHeap) Peek() *reader {
	return h[0]
}

type reader struct {
	buf       *Buffer
	i         int
	off       int
	size      int
	data      Reader
	closeOnce sync.Once
	life
}

func (r *reader) Read(p []byte) (n int, err error) {
	if r.data.Len() == 0 {
		r.buf.fetch(r)
	}
	n, err = r.data.Read(p)
	if err == io.EOF {
		if !r.alive() {
			return n, err
		} else if r.buf.alive() {
			err = nil
		} else {
			r.buf.fetch(r)
			if r.data.Len() > 0 {
				err = nil
			}
		}
	}
	return n, err
}

// break calls to read.
func (r *reader) Close() error {
	r.closeOnce.Do(func() {
		r.kill()
		r.buf.drop(r)
	})
	return nil
}

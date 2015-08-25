package bufit

import "io"

type writer struct {
	empty     bool
	off, roff int
	data      []byte
}

func newWriter(p []byte) *writer {
	return &writer{
		empty: len(p) == 0,
		off:   len(p),
		data:  p[0:cap(p)],
	}
}

func split(a, b int, p []byte) (as, bs []byte) {
	if a < b {
		return p[a:b], nil
	} else {
		return p[a:], p[0:b]
	}
}

func (buf *writer) Len() int {
	if buf.empty {
		return 0
	} else if buf.roff < buf.off {
		return buf.off - buf.roff
	} else {
		return len(buf.data) - buf.roff + buf.off
	}
}

func (buf *writer) Cap() int {
	return cap(buf.data)
}

func (buf *writer) grow(s int) *writer {
	c, l := buf.Cap(), buf.Len()
	if c-l >= s {
		return buf
	}
	next := newWriter(make([]byte, 0, c*2+s))
	if !buf.empty {
		a, b := split(buf.roff, buf.off, buf.data)
		next.Write(a)
		next.Write(b)
	}
	return next
}

// no bounds check, expected.
func (buf *writer) Discard(s int) (n int, err error) {
	if s > 0 {
		buf.roff = (buf.roff + s) % cap(buf.data)
		if buf.roff == buf.off {
			err = io.EOF
			buf.empty = true
		}
	}
	return s, err
}

func (buf *writer) Write(p []byte) (n int, err error) {
	*buf = *buf.grow(len(p))
	a, b := split(buf.off, buf.roff, buf.data)
	n = copy(a, p)
	if n < len(p) {
		n += copy(b, p[n:])
	}
	if n > 0 {
		buf.empty = false
	}
	buf.off = (buf.off + n) % cap(buf.data)
	return n, err
}

func (buf *writer) Read(p []byte) (n int, err error) {
	if buf.empty {
		return 0, io.EOF
	}
	a, b := split(buf.roff, buf.off, buf.data)
	n = copy(p, a)
	if n < len(p) {
		n += copy(p[n:], b)
	}
	return buf.Discard(n)
}

func (buf *writer) ReadAt(p []byte, off int64) (n int, err error) {
	if buf.empty {
		return 0, io.EOF
	}
	a, b := split(buf.roff, buf.off, buf.data)
	if int64(len(a)) > off {
		a = a[off:]
	} else if int64(len(b)) > off-int64(len(a)) {
		b = b[off-int64(len(a)):]
		a = nil
	} else {
		return 0, io.EOF
	}
	n = copy(p, a)
	if n < len(p) {
		n += copy(p[n:], b)
	}
	if n < len(p) {
		err = io.EOF
	}
	return n, err
}

func (buf writer) NextReader() Reader { return &buf }

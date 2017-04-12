package bufit

import (
	"bytes"
	"crypto/rand"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"
)

func ExampleWriter() {
	buf := newWriter(make([]byte, 0, 10))
	io.Copy(os.Stdout, buf)
	io.Copy(os.Stdout, io.NewSectionReader(*&buf, 0, 100))

	io.WriteString(buf, "Hello ")
	r := io.NewSectionReader(*&buf, 0, int64(buf.Len()))
	io.CopyN(os.Stdout, r, 5)
	io.CopyN(os.Stdout, buf, 5)
	io.WriteString(buf, "World")
	r = io.NewSectionReader(*&buf, 0, int64(buf.Len()))
	io.CopyN(os.Stdout, r, 6)

	io.WriteString(buf, "abcdefg")
	io.Copy(os.Stdout, buf)
	io.Copy(os.Stdout, buf)

	io.WriteString(buf, "Hello World")
	r = io.NewSectionReader(*&buf, 0, int64(buf.Len()))
	io.CopyN(os.Stdout, r, 5)
	io.CopyN(os.Stdout, buf, 4)

	io.WriteString(buf, "abcdefg")
	io.Copy(os.Stdout, buf)
	io.Copy(os.Stdout, buf)
	//Output:
	// HelloHello World WorldabcdefgHelloHello Worldabcdefg
}

type badBuffer []byte

func (b *badBuffer) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}

func (b *badBuffer) Read(p []byte) (n int, err error) {
	n = copy(p, *b)
	*b = (*b)[n:]
	if len(*b) == 0 {
		err = io.EOF
	}
	return n, err
}

func BenchmarkBuffer(b *testing.B) {
	buf := New()
	data, _ := ioutil.ReadAll(io.LimitReader(rand.Reader, 32*1024))

	go func() {
		for i := 0; i < 1000; i++ {
			buf.Write(data)
		}
		buf.Close()
	}()

	r := buf.NextReader()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rr := buf.NextReader()
			io.Copy(ioutil.Discard, rr)
			rr.Close()
		}
	})

	r.Close()
	b.ReportAllocs()
}

func BenchmarkReadWriter(b *testing.B) {
	buf := newWriter(nil)
	data, _ := ioutil.ReadAll(io.LimitReader(rand.Reader, 32*1024))
	temp := make([]byte, 32*1024)
	for i := 0; i < b.N; i++ {
		buf.Write(data)
		io.CopyBuffer(ioutil.Discard, buf, temp)
	}
	b.ReportAllocs()
}

func TestConcurrent(t *testing.T) {
	var grp sync.WaitGroup
	buf := New()

	var rs []io.ReadCloser
	for i := 0; i < 1000; i++ {
		rs = append(rs, buf.NextReader())
	}

	testData := bytes.NewBuffer(nil)
	io.CopyN(testData, rand.Reader, 32*1024*10)

	for _, r := range rs {
		grp.Add(1)
		go func(r io.ReadCloser) {
			defer grp.Done()
			defer r.Close()
			data, err := ioutil.ReadAll(r)
			if err != nil {
				t.Error(err)
			}
			if !bytes.Equal(testData.Bytes(), data) {
				t.Error("unexpected result...", testData.Len(), len(data))
			}
		}(r)
	}

	r := bytes.NewReader(testData.Bytes())
	for r.Len() > 0 {
		io.CopyN(buf, r, 32*1024*2)
		<-time.After(100 * time.Millisecond)
	}
	buf.Close()
	grp.Wait()
}

func TestQuit(t *testing.T) {
	buf := New()
	r := buf.NextReader()

	wait := make(chan struct{})
	go func() {
		io.Copy(ioutil.Discard, r)
		close(wait)
	}()

	r.Close()
	select {
	case <-wait:
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for Reader to Close")
	}
}

func ExampleBuffer() {
	// Start a new buffer
	buf := New()

	// Create two readers
	r1, r2 := buf.NextReader(), buf.NextReader()

	// Broadcast a message
	io.WriteString(buf, "Hello World\n")

	// Wait
	var grp sync.WaitGroup
	grp.Add(4)

	// Read fast
	go func() {
		defer grp.Done()
		io.Copy(os.Stdout, r1) // "Hello World\n"
	}()

	// Read slow
	go func() {
		defer grp.Done()
		<-time.After(100 * time.Millisecond)
		io.CopyN(os.Stdout, r2, 5) // "Hello"
		<-time.After(time.Second)
		io.Copy(os.Stdout, r2) // "World\n"
	}()

	// Both readers will read the entire buffer! The slow reader
	// won't block the fast one from reading ahead either.

	// Late reader
	// Since this reader joins after all existing readers have Read "Hello"
	// "Hello" has already been cleared from the Buffer, this Reader will only see
	// "World\n" and beyond.
	go func() {
		defer grp.Done()
		<-time.After(500 * time.Millisecond)
		r3 := buf.NextReader()
		io.Copy(os.Stdout, r3) // "World\n"
	}()

	// Short Reader
	// **Important!** if your reader isn't going to read until the buffer is empty
	// you'll need to call Close() when you are done with it to tell the buffer
	// it's done reading data.
	go func() {
		defer grp.Done()
		<-time.After(100 * time.Millisecond)
		r4 := buf.NextReader()
		io.CopyN(os.Stdout, r4, 5) // "Hello"
		r4.Close()                 // tell the buffer you're done reading
	}()

	// **Important!** mark close so that readers can ret. io.EOF
	buf.Close()

	grp.Wait()
	// Output:
	// Hello World
	// HelloHelloHello World
	//  World
}

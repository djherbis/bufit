package bufit

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"
)

func checker(t *testing.T, r1, r2 io.Reader) chan struct{} {
	c := make(chan struct{})
	go func() {
		a, er1 := ioutil.ReadAll(r1)
		b, er2 := ioutil.ReadAll(r2)
		if er1 != er2 {
			t.Error("mismatched ers", er1, er2)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("mismatch output [%s] [%s]\n", string(a), string(b))
		}
		close(c)
	}()
	return c
}

const output2 = `Hello Grace
Hello World
Hello Grace
Hello World
Hello Dustin
Hello Dustin
Hello`

func TestJoin(t *testing.T) {
	wt := 500 * time.Millisecond

	var grp sync.WaitGroup
	grp.Add(3)

	var grp2 sync.WaitGroup
	grp2.Add(2)

	r, w := io.Pipe()
	c := checker(t, r, bytes.NewBufferString(output2))

	b := New()
	r1, r2 := b.NextReader(), b.NextReader()
	defer r1.Close()
	defer r2.Close()
	io.WriteString(b, "Hello Grace\n")
	go func() {
		io.WriteString(b, "Hello World\n")
		<-time.After(wt)
		io.WriteString(b, "Hello Dustin\n")
		b.Close()
	}()
	go func() {
		defer grp.Done()
		<-time.After(wt)
		r := b.NextReader()
		<-time.After(wt)
		grp2.Wait()
		io.CopyN(w, r, 5)
		r.Close()
	}()
	go func() {
		io.Copy(w, r1)
		grp.Done()
		grp2.Done()
	}()
	io.Copy(w, r2)
	grp.Done()
	grp2.Done()
	grp.Wait()
	w.Close()
	<-c
	if len(b.data) > 0 {
		t.Error("leftovers")
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

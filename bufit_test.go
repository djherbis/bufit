package bufit

import (
	"io"
	"os"
	"sync"
	"time"
)

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

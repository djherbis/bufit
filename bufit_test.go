package bufit

import (
	"bytes"
	"io"
	"io/ioutil"
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
	wt := 100 * time.Millisecond

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

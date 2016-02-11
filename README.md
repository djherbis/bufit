Bufit
==========

[![GoDoc](https://godoc.org/github.com/djherbis/bufit?status.svg)](https://godoc.org/github.com/djherbis/bufit)
[![Release](https://img.shields.io/github/release/djherbis/bufit.svg)](https://github.com/djherbis/bufit/releases/latest)
[![Software License](https://img.shields.io/badge/license-MIT-brightgreen.svg)](LICENSE.txt)
[![Build Status](https://travis-ci.org/djherbis/bufit.svg?branch=master)](https://travis-ci.org/djherbis/bufit) 
[![Coverage Status](https://coveralls.io/repos/djherbis/bufit/badge.svg?branch=master)](https://coveralls.io/r/djherbis/bufit?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/djherbis/bufit)](https://goreportcard.com/report/github.com/djherbis/bufit)

Usage
------------
A moving buffer which supports multiple concurrent readers.

This buffer shares a single in-memory buffer with multiple readers who can independently Read from the buffer.

```go

import (
  "io"
  "sync"

  "github.com/djherbis/bufit"
)

func main(){
  // Start a new buffer
  buf := bufit.New()

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
}

```

Installation
------------
```sh
go get github.com/djherbis/bufit
```

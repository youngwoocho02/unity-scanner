package cmd

import (
	"bytes"
	"io"
	"os"
)

func captureStdout(buf *bytes.Buffer) func() {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()

	return func() {
		_ = w.Close()
		<-done
		os.Stdout = old
	}
}

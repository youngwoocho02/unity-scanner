package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
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

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

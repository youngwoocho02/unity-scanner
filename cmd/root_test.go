package cmd

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReorderFlagArgsAllowsFlagsAfterPath(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var project, kind string
	var flat bool
	fs.StringVar(&project, "p", "", "")
	fs.StringVar(&kind, "kind", "", "")
	fs.BoolVar(&flat, "flat", false, "")

	got := reorderFlagArgs(fs, []string{"Assets/Foo", "--kind", "prefab", "--flat", "-p", "C:/Project"})
	want := []string{"--kind", "prefab", "--flat", "-p", "C:/Project", "Assets/Foo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestNormalCommandsDoNotWriteUpdateCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	origVersion := Version
	Version = "v0.1.0"
	t.Cleanup(func() { Version = origVersion })

	project := t.TempDir()
	assets := filepath.Join(project, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Foo.prefab"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	err := Execute([]string{"list", "-p", project, "--flat", "--limit", "1", "Assets"})
	restoreStdout()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".unity-scanner")); !os.IsNotExist(err) {
		t.Fatalf("expected no update cache directory, stat err=%v", err)
	}
}

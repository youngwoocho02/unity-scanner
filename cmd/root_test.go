package cmd

import (
	"bytes"
	"flag"
	"fmt"
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

func TestScanCommandsDoNotWriteUpdateCache(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(assets, "Config.asset.meta"), []byte("guid: abcdef1234567890abcdef1234567890\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Config.asset"), []byte(`%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Name: Config
  ref: {fileID: 1, guid: abcdef1234567890abcdef1234567890, type: 2}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	commands := [][]string{
		{"list", "-p", project, "--flat", "--limit", "1", "Assets"},
		{"read", "-p", project, "Assets/Config.asset", "--field-limit", "1"},
		{"search", "-p", project, "--name", "Config", "--type", "asset", "--limit", "1", "Assets"},
		{"refs", "-p", project, "Assets/Config.asset", "Assets", "--limit", "1"},
	}
	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			var buf bytes.Buffer
			restoreStdout := captureStdout(&buf)
			err := Execute(args)
			restoreStdout()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(home, ".unity-scanner")); !os.IsNotExist(err) {
				t.Fatalf("expected no update cache directory after %s, stat err=%v", fmt.Sprint(args), err)
			}
		})
	}
}

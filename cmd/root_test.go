package cmd

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestHelpIncludesCommandDiscovery(t *testing.T) {
	out := executeOutput(t)
	for _, want := range []string{
		"unity-scanner help [command]",
		"list     compressed ls for Unity assets (alias: ls)",
		"search   structured name/component/guid search (alias: find)",
		"help     show general help or command help",
		"version  print version",
		"Root options:\n  -h, --help             Show help",
		"Project commands:\n  -p, --project <path>   Unity project path",
		"-h, --help             Show help",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q\n%s", want, out)
		}
	}
}

func TestTopicHelpIncludesCommonFlagsAndExamples(t *testing.T) {
	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"help", "list"}, []string{"Usage:\n  unity-scanner list", "Aliases:\n  ls", "-p, --project <path>", "unity-scanner ls -p ."}},
		{[]string{"read", "--help"}, []string{"Usage:\n  unity-scanner read", "Aliases:\n  cat", "--full-tree", "unity-scanner cat -p ."}},
		{[]string{"search", "Assets", "--help"}, []string{"Usage:\n  unity-scanner search", "Aliases:\n  find", "--ref <guid>", "unity-scanner find -p ."}},
		{[]string{"refs", "-h"}, []string{"Usage:\n  unity-scanner refs", "-p, --project <path>", "--detail", "0123456789abcdef0123456789abcdef"}},
		{[]string{"update", "--help"}, []string{"Usage:\n  unity-scanner update", "-h, --help", "--check", "unity-scanner update --check"}},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			out := executeOutput(t, tt.args...)
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Fatalf("help missing %q\n%s", want, out)
				}
			}
		})
	}
}

func TestPrintLineLimitedTruncatesLongLine(t *testing.T) {
	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	printLineLimited(8, "abcdefghijklmnop")
	restoreStdout()
	if got := strings.TrimSpace(buf.String()); got != "abcde..." {
		t.Fatalf("line=%q", got)
	}
}

func TestPrintLineLimitedCanBeDisabled(t *testing.T) {
	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	printLineLimited(0, "abcdefghijklmnop")
	restoreStdout()
	if got := strings.TrimSpace(buf.String()); got != "abcdefghijklmnop" {
		t.Fatalf("line=%q", got)
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

func executeOutput(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	err := Execute(args)
	restoreStdout()
	if err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

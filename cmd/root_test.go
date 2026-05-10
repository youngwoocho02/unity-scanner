package cmd

import (
	"flag"
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

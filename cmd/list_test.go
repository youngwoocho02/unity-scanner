package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

func TestPrintListUsesRootRelativeGroupsAndOneExtBlock(t *testing.T) {
	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)

	result := unityasset.ScanResult{
		MetaCount: 2,
		KindCount: map[string]int{"prefab": 2},
		Files: []unityasset.FileEntry{
			{AssetPath: "Assets/Foo/A_01.prefab", Dir: "Assets/Foo", Name: "A_01", Kind: "prefab"},
			{AssetPath: "Assets/Foo/A_02.prefab", Dir: "Assets/Foo", Name: "A_02", Kind: "prefab"},
		},
	}

	printList("Assets/Foo", result, listOptions{flat: true, limit: 10})
	restoreStdout()
	out := buf.String()
	for _, noisy := range []string{"PROJECT", "ROOT", "META", "FILES"} {
		if strings.Contains(out, noisy) {
			t.Fatalf("unexpected %s header:\n%s", noisy, out)
		}
	}
	if !strings.Contains(out, "EXT\n  prefab") {
		t.Fatalf("missing ext declaration:\n%s", out)
	}
	if !strings.Contains(out, "GROUPS\n  .  [prefab]") {
		t.Fatalf("missing root-relative group:\n%s", out)
	}
	if strings.Contains(out, ".prefab\n    A_01") {
		t.Fatalf("extension leaked into names:\n%s", out)
	}
}

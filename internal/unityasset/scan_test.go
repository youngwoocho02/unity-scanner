package unityasset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSingleFileAppliesKindFilter(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	prefab := filepath.Join(assets, "Foo.prefab")
	if err := os.WriteFile(prefab, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	project, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Scan(project, "Assets/Foo.prefab", ScanOptions{Kinds: ParseKindSet("scene")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("got %d files, want 0", len(result.Files))
	}
}

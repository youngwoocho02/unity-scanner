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

func TestScanCountsIncludedMetaOnce(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Foo.prefab"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Foo.prefab.meta"), []byte("guid: abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	project, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Scan(project, "Assets", ScanOptions{IncludeMeta: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.MetaCount != 1 {
		t.Fatalf("meta count=%d", result.MetaCount)
	}
	if result.KindCount["meta"] != 1 {
		t.Fatalf("kind meta=%d", result.KindCount["meta"])
	}
	if result.KindCount["prefab"] != 1 {
		t.Fatalf("kind prefab=%d", result.KindCount["prefab"])
	}
	if len(result.Files) != 2 {
		t.Fatalf("files=%d", len(result.Files))
	}
}

func TestScanParallelKeepsSortedResultsAndSkipsBuildDirs(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	for _, path := range []string{
		filepath.Join(assets, "B", "Two.prefab"),
		filepath.Join(assets, "A", "One.prefab"),
		filepath.Join(assets, "Temp", "Ignored.prefab"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	project, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Scan(project, "Assets", ScanOptions{Kinds: ParseKindSet("prefab"), Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("files=%d", len(result.Files))
	}
	if result.Files[0].AssetPath != "Assets/A/One.prefab" || result.Files[1].AssetPath != "Assets/B/Two.prefab" {
		t.Fatalf("files not sorted or skipped: %#v", result.Files)
	}
}

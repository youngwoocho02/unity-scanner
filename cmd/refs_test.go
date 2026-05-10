package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

func TestResolveRefGUIDFromAssetMeta(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Foo.asset"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Foo.asset.meta"), []byte("guid: ABCDEF1234567890ABCDEF1234567890\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	project, err := unityasset.OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	guid, label, err := resolveRefGUID(project, "Assets/Foo.asset")
	if err != nil {
		t.Fatal(err)
	}
	if guid != "abcdef1234567890abcdef1234567890" {
		t.Fatalf("guid=%q", guid)
	}
	if label != "Assets/Foo.asset" {
		t.Fatalf("label=%q", label)
	}
}

func TestResolveRefGUIDVariants(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(assets, "Foo.asset.meta")
	if err := os.WriteFile(filepath.Join(assets, "Foo.asset"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(meta, []byte("guid: ABCDEF1234567890ABCDEF1234567890\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := unityasset.OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	guid, label, err := resolveRefGUID(project, "Assets/Foo.asset.meta")
	if err != nil {
		t.Fatal(err)
	}
	if guid != "abcdef1234567890abcdef1234567890" || label != "Assets/Foo.asset" {
		t.Fatalf("guid=%q label=%q", guid, label)
	}

	guid, label, err = resolveRefGUID(project, "ABCDEF1234567890ABCDEF1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if guid != "abcdef1234567890abcdef1234567890" || label != "guid" {
		t.Fatalf("guid=%q label=%q", guid, label)
	}
}

func TestRefsCmdPrintsCompactReference(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Target.asset"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Target.asset.meta"), []byte("guid: abcdef1234567890abcdef1234567890\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "User.asset"), []byte(`%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Name: User
  target: {fileID: 11400000, guid: abcdef1234567890abcdef1234567890, type: 2}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	err := refsCmd([]string{"-p", dir, "Assets/Target.asset", "Assets", "--limit", "5"})
	restoreStdout()
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"REF     Assets/Target.asset", "GUID    abcdef1234567890abcdef1234567890", "[asset]", ". :: User"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

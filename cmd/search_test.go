package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

func TestFileContainsFindsNeedleAcrossChunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Large.asset")
	data := make([]byte, textSearchChunkSize+4)
	for i := range data {
		data[i] = 'x'
	}
	copy(data[textSearchChunkSize-2:], []byte("needle"))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := fileContains(unityasset.FileEntry{Abs: path, Kind: "asset"}, "needle")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func BenchmarkFileContainsLargeAsset(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "Large.asset")
	needle := "abcdef1234567890abcdef1234567890"
	data := make([]byte, textSearchChunkSize*8)
	for i := range data {
		data[i] = 'x'
	}
	copy(data[len(data)-len(needle):], needle)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatal(err)
	}
	file := unityasset.FileEntry{Abs: path, Kind: "asset"}

	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ok, err := fileContains(file, needle)
		if err != nil || !ok {
			b.Fatalf("ok=%v err=%v", ok, err)
		}
	}
}

func BenchmarkRunSearchGUIDManyFiles(b *testing.B) {
	dir := b.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		b.Fatal(err)
	}
	needle := "abcdef1234567890abcdef1234567890"
	for i := 0; i < 128; i++ {
		body := strings.Repeat("x", 64*1024)
		if i%16 == 0 {
			body += needle
		}
		path := filepath.Join(assets, fmt.Sprintf("File_%03d.prefab", i))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	project, err := unityasset.OpenProject(dir)
	if err != nil {
		b.Fatal(err)
	}
	result, err := unityasset.Scan(project, "Assets", unityasset.ScanOptions{Kinds: unityasset.ParseKindSet("prefab")})
	if err != nil {
		b.Fatal(err)
	}
	opts := searchOptions{guid: needle}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches, warnings := runSearch(project, result.Files, unityasset.ScriptIndex{}, opts)
		if len(matches) != 8 || len(warnings) != 0 {
			b.Fatalf("matches=%d warnings=%d", len(matches), len(warnings))
		}
	}
}

func TestObjectMatchesScriptableAssetComponent(t *testing.T) {
	asset, err := unityasset.ParseAsset([]byte(`%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Script: {fileID: 11500000, guid: abcdef123456, type: 3}
  m_Name: Character
`))
	if err != nil {
		t.Fatal(err)
	}
	asset.ScriptIndex = unityasset.ScriptIndex{"abcdef123456": "Assets/Scripts/SO_CharacterConfig.cs"}

	matches := objectMatches(asset, searchOptions{name: "Character", component: "SO_CharacterConfig"})
	if len(matches) != 1 {
		t.Fatalf("matches=%#v", matches)
	}
}

func TestRunSearchRequiresComponentWhenNameAndComponentProvided(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	prefab := filepath.Join(assets, "Blender.prefab")
	if err := os.WriteFile(prefab, []byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  m_Name: Blender
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	project, err := unityasset.OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := unityasset.Scan(project, "Assets", unityasset.ScanOptions{Kinds: unityasset.ParseKindSet("prefab")})
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := runSearch(project, result.Files, unityasset.ScriptIndex{}, searchOptions{name: "Blender", component: "Missing"})
	if len(matches) != 0 {
		t.Fatalf("matches=%#v", matches)
	}
}

func TestRunSearchParallelKeepsOrderAndWarnings(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		path := filepath.Join(assets, fmt.Sprintf("File_%02d.prefab", i))
		body := `%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  m_Name: Target
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
`
		if i%2 == 0 {
			body = strings.Replace(body, "Target", "Other", 1)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	project, err := unityasset.OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := unityasset.Scan(project, "Assets", unityasset.ScanOptions{Kinds: unityasset.ParseKindSet("prefab")})
	if err != nil {
		t.Fatal(err)
	}
	matches, warnings := runSearch(project, result.Files, unityasset.ScriptIndex{}, searchOptions{name: "Target"})
	if len(warnings) != 0 {
		t.Fatalf("warnings=%#v", warnings)
	}
	if len(matches) != 20 {
		t.Fatalf("matches=%d", len(matches))
	}
	for i := 1; i < len(matches); i++ {
		if matches[i-1].File.AssetPath > matches[i].File.AssetPath {
			t.Fatalf("matches out of order: %s > %s", matches[i-1].File.AssetPath, matches[i].File.AssetPath)
		}
	}
}

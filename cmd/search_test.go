package cmd

import (
	"bytes"
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

func TestFileContainsEdgeCases(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "Small.asset")
	if err := os.WriteFile(textPath, []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		file unityasset.FileEntry
		want bool
	}{
		{name: "start", file: unityasset.FileEntry{Abs: textPath, Kind: "asset"}, want: true},
		{name: "absent", file: unityasset.FileEntry{Abs: textPath, Kind: "asset"}, want: false},
		{name: "non-text-skip", file: unityasset.FileEntry{Abs: filepath.Join(dir, "missing.png"), Kind: "png"}, want: false},
	}
	needles := map[string]string{
		"start":         "alpha",
		"absent":        "delta",
		"non-text-skip": "anything",
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := fileContains(tt.file, needles[tt.name])
			if err != nil {
				t.Fatal(err)
			}
			if ok != tt.want {
				t.Fatalf("ok=%v want=%v", ok, tt.want)
			}
		})
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

func TestRunSearchScopedComponentDoesNotMatchMissingMonoBehaviourFallback(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	scripts := filepath.Join(assets, "Scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "TargetStation.cs.meta"), []byte("guid: abcdef123456\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "Other.cs.meta"), []byte("guid: fedcba654321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prefab := filepath.Join(assets, "Item.prefab")
	if err := os.WriteFile(prefab, []byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: 300}
  m_Name: Item
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: fedcba654321, type: 3}
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
	scriptsIndex, err := unityasset.BuildScriptIndexForQuery(project, "TargetStation")
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := runSearch(project, result.Files, scriptsIndex, searchOptions{component: "TargetStation", scriptScoped: true})
	if len(matches) != 0 {
		t.Fatalf("matches=%#v", matches)
	}
	matches, _ = runSearch(project, result.Files, unityasset.ScriptIndex{}, searchOptions{component: "MonoBehaviour", scriptScoped: true})
	if len(matches) != 0 {
		t.Fatalf("fallback matches=%#v", matches)
	}
}

func TestSearchCmdMatchesScriptComponentWithoutPathFalsePositive(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	writeTestFile(t, filepath.Join(assets, "Scripts", "TargetStation.cs.meta"), "guid: abcdef1234567890abcdef1234567890\n")
	writeTestFile(t, filepath.Join(assets, "OfficeAndPoliceStation", "Scripts", "LightOptimize.cs.meta"), "guid: fedcba654321fedcba654321fedcba65\n")
	writeTestFile(t, filepath.Join(assets, "Target.prefab"), `%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: 300}
  m_Name: Target
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: abcdef1234567890abcdef1234567890, type: 3}
`)
	writeTestFile(t, filepath.Join(assets, "FalsePositive.prefab"), `%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: 300}
  m_Name: FalsePositive
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: fedcba654321fedcba654321fedcba65, type: 3}
`)

	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	err := searchCmd([]string{"-p", dir, "--component", "Station", "--type", "prefab", "--limit", "20", "Assets"})
	restoreStdout()
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "MATCHES  1") || !strings.Contains(out, "components: Transform, TargetStation") {
		t.Fatalf("missing target component:\n%s", out)
	}
	if strings.Contains(out, "FalsePositive") || strings.Contains(out, "LightOptimize") {
		t.Fatalf("path/name false positive leaked:\n%s", out)
	}
}

func TestSearchCmdMatchesNativeComponentWithScopedScripts(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	writeTestFile(t, filepath.Join(assets, "UI.prefab"), `%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: 300}
  m_Name: CanvasRoot
--- !u!224 &200
RectTransform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!223 &300
Canvas:
  m_GameObject: {fileID: 100}
`)

	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	err := searchCmd([]string{"-p", dir, "--component", "Canvas", "--type", "prefab", "--limit", "20", "Assets"})
	restoreStdout()
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "MATCHES  1") || !strings.Contains(out, "components: RectTransform, Canvas") {
		t.Fatalf("native component not found:\n%s", out)
	}
}

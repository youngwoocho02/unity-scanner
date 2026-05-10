package unityasset

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseAssetHierarchyAndComponents(t *testing.T) {
	data := []byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: 300}
  m_Name: Root
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: abcdef123456, type: 3}
  publicValue: 42
--- !u!1 &101
GameObject:
  m_Component:
  - component: {fileID: 201}
  m_Name: Child
--- !u!4 &201
Transform:
  m_GameObject: {fileID: 101}
  m_Father: {fileID: 200}
`)

	asset, err := ParseAsset(data)
	if err != nil {
		t.Fatal(err)
	}
	asset.ScriptIndex = ScriptIndex{"abcdef123456": "Assets/Scripts/Foo.cs"}

	roots := asset.Hierarchy()
	if len(roots) != 1 {
		t.Fatalf("roots=%d", len(roots))
	}
	if roots[0].Path != "Root" || roots[0].Children[0].Path != "Root/Child" {
		t.Fatalf("unexpected hierarchy: %#v", roots[0])
	}

	components := asset.ComponentsFor("100")
	if got := components[1].Name; got != "Foo" {
		t.Fatalf("component=%q", got)
	}

	fields := asset.Fields(components[1].Object, 10)
	if len(fields) != 1 || fields[0].Name != "publicValue" {
		t.Fatalf("fields=%#v", fields)
	}
}

func TestParseScriptableAssetFields(t *testing.T) {
	data := []byte(`%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Script: {fileID: 11500000, guid: abcdef123456, type: 3}
  m_Name: Config
  scalarValue: 42
  nestedRef:
    fileID: 123
    guid: fedcba654321
`)

	asset, err := ParseAsset(data)
	if err != nil {
		t.Fatal(err)
	}
	asset.ScriptIndex = ScriptIndex{"abcdef123456": "Assets/Scripts/ConfigAsset.cs"}

	if got := len(asset.GameObjects()); got != 0 {
		t.Fatalf("game objects=%d", got)
	}
	name, _ := asset.ComponentName(asset.Objects[0])
	if name != "ConfigAsset" {
		t.Fatalf("name=%q", name)
	}
	fields := asset.Fields(asset.Objects[0], 10)
	if len(fields) != 2 {
		t.Fatalf("fields=%#v", fields)
	}
	if fields[1].Value == "<object>" {
		t.Fatalf("nested field was not summarized: %#v", fields[1])
	}
}

func TestResolveReferences(t *testing.T) {
	asset := &Asset{
		GUIDIndex: GUIDIndex{
			"abcdef1234567890abcdef1234567890": "Assets/Data/Foo.asset",
		},
	}

	got := asset.ResolveReferences("{fileID: 1, guid: abcdef1234567890abcdef1234567890, type: 2}")
	want := "{fileID: 1, guid: abcdef1234567890abcdef1234567890, type: 2} -> Assets/Data/Foo.asset"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAddFieldGUIDsSkipsScriptGUID(t *testing.T) {
	obj := &Object{Lines: []string{
		"  m_Script: {fileID: 11500000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}",
		"  icon: {fileID: 1, guid: BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB, type: 3}",
	}}
	guids := map[string]bool{}

	AddFieldGUIDs(guids, obj)
	if len(guids) != 1 || !guids["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] {
		t.Fatalf("guids=%#v", guids)
	}
}

func TestFieldsWithHidden(t *testing.T) {
	asset := &Asset{}
	obj := &Object{Lines: []string{
		"MonoBehaviour:",
		"  first: 1",
		"  second: 2",
		"  third: 3",
	}}

	fields, hidden := asset.FieldsWithHidden(obj, 2)
	if len(fields) != 2 || hidden != 1 {
		t.Fatalf("fields=%#v hidden=%d", fields, hidden)
	}
}

func TestScriptGUIDs(t *testing.T) {
	asset, err := ParseAsset([]byte(`%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Script: {fileID: 11500000, guid: abcdef123456, type: 3}
  m_Name: Config
--- !u!114 &11400001
MonoBehaviour:
  m_Script: {fileID: 11500000, guid: abcdef123456, type: 3}
  m_Name: ConfigTwo
`))
	if err != nil {
		t.Fatal(err)
	}

	guids := asset.ScriptGUIDs()
	if len(guids) != 1 || !guids["abcdef123456"] {
		t.Fatalf("guids=%#v", guids)
	}
}

func TestParseAssetSummaryOmitsLinesButKeepsStructure(t *testing.T) {
	asset, err := ParseAssetSummary([]byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: 300}
  m_Name: Root
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: ABCDEF123456, type: 3}
`))
	if err != nil {
		t.Fatal(err)
	}

	if len(asset.Objects[0].Lines) != 0 {
		t.Fatalf("summary parse kept lines: %#v", asset.Objects[0].Lines)
	}
	if asset.Objects[0].Name != "Root" || len(asset.Objects[0].ComponentIDs) != 2 {
		t.Fatalf("game object=%#v", asset.Objects[0])
	}
	if asset.Objects[1].GameObjectID != "100" || asset.Objects[1].FatherTransformID != "0" {
		t.Fatalf("transform=%#v", asset.Objects[1])
	}
	if asset.Objects[2].ScriptGUID != "abcdef123456" {
		t.Fatalf("script guid=%q", asset.Objects[2].ScriptGUID)
	}
}

func TestBuildScriptIndexForGUIDs(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "Assets", "Scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "Foo.cs.meta"), []byte("guid: abcdef123456\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "Bar.cs.meta"), []byte("guid: fedcba654321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	index, err := BuildScriptIndexForGUIDs(project, map[string]bool{"abcdef123456": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || index["abcdef123456"] != "Assets/Scripts/Foo.cs" {
		t.Fatalf("index=%#v", index)
	}
}

func TestBuildScriptIndexForQueryOnlyReadsMatchingScripts(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "Assets", "Scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "TargetStation.cs.meta"), []byte("guid: abcdef123456\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "Other.cs.meta"), []byte("guid: fedcba654321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stationDir := filepath.Join(dir, "Assets", "OfficeAndPoliceStation", "Scripts")
	if err := os.MkdirAll(stationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stationDir, "LightOptimize.cs.meta"), []byte("guid: 111111111111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	index, err := BuildScriptIndexForQuery(project, "station")
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || index["abcdef123456"] != "Assets/Scripts/TargetStation.cs" {
		t.Fatalf("index=%#v", index)
	}
}

func TestBuildGUIDIndexForGUIDs(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets", "Data")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Target.asset.meta"), []byte("guid: abcdef123456\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "Other.asset.meta"), []byte("guid: fedcba654321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}

	index, err := BuildGUIDIndexForGUIDs(project, map[string]bool{"abcdef123456": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || index["abcdef123456"] != "Assets/Data/Target.asset" {
		t.Fatalf("index=%#v", index)
	}
}

func BenchmarkParseAssetLargePrefab(b *testing.B) {
	data := []byte(largePrefabYAML(1000))

	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		asset, err := ParseAsset(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(asset.Objects) != 2000 {
			b.Fatalf("objects=%d", len(asset.Objects))
		}
	}
}

func BenchmarkParseAssetLargePrefabSummary(b *testing.B) {
	data := []byte(largePrefabYAML(1000))

	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		asset, err := ParseAssetSummary(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(asset.Objects) != 2000 {
			b.Fatalf("objects=%d", len(asset.Objects))
		}
	}
}

func BenchmarkBuildScriptIndex(b *testing.B) {
	project := benchmarkScriptProject(b, 1000)

	b.Run("all", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			index, err := BuildScriptIndex(project)
			if err != nil {
				b.Fatal(err)
			}
			if len(index) != 1000 {
				b.Fatalf("index=%d", len(index))
			}
		}
	})

	b.Run("targeted", func(b *testing.B) {
		wanted := map[string]bool{"00000000000000000000000000000000": true}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			index, err := BuildScriptIndexForGUIDs(project, wanted)
			if err != nil {
				b.Fatal(err)
			}
			if len(index) != 1 {
				b.Fatalf("index=%d", len(index))
			}
		}
	})

	b.Run("query", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			index, err := BuildScriptIndexForQuery(project, "Script_0000")
			if err != nil {
				b.Fatal(err)
			}
			if len(index) != 1 {
				b.Fatalf("index=%d", len(index))
			}
		}
	})
}

func largePrefabYAML(count int) string {
	var b strings.Builder
	b.WriteString("%YAML 1.1\n")
	for i := 0; i < count; i++ {
		goID := 100000 + i
		transformID := 200000 + i
		parentID := 0
		if i > 0 {
			parentID = 200000 + i - 1
		}
		b.WriteString("--- !u!1 &")
		b.WriteString(strconv.Itoa(goID))
		b.WriteString("\nGameObject:\n  m_Component:\n  - component: {fileID: ")
		b.WriteString(strconv.Itoa(transformID))
		b.WriteString("}\n  m_Name: Node_")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\n--- !u!4 &")
		b.WriteString(strconv.Itoa(transformID))
		b.WriteString("\nTransform:\n  m_GameObject: {fileID: ")
		b.WriteString(strconv.Itoa(goID))
		b.WriteString("}\n  m_Father: {fileID: ")
		b.WriteString(strconv.Itoa(parentID))
		b.WriteString("}\n")
	}
	return b.String()
}

func benchmarkScriptProject(b *testing.B, count int) Project {
	b.Helper()
	dir := b.TempDir()
	scripts := filepath.Join(dir, "Assets", "Scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < count; i++ {
		guid := fmt.Sprintf("%032x", i)
		path := filepath.Join(scripts, fmt.Sprintf("Script_%04d.cs.meta", i))
		if err := os.WriteFile(path, []byte("guid: "+guid+"\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	project, err := OpenProject(dir)
	if err != nil {
		b.Fatal(err)
	}
	return project
}

package unityasset

import (
	"os"
	"path/filepath"
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

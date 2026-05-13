package unityasset

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestComponentsForIncludesAddedComponentByGameObjectReference(t *testing.T) {
	data := []byte(`%YAML 1.1
--- !u!1 &100 stripped
GameObject:
  m_CorrespondingSourceObject: {fileID: 200, guid: abcdef123456, type: 3}
  m_PrefabInstance: {fileID: 100100000}
--- !u!65 &300
BoxCollider:
  m_GameObject: {fileID: 100}
  m_Size: {x: 1, y: 2, z: 3}
`)

	asset, err := ParseAsset(data)
	if err != nil {
		t.Fatal(err)
	}

	components := asset.ComponentsFor("100")
	if len(components) != 1 || components[0].Name != "BoxCollider" {
		t.Fatalf("components=%#v", components)
	}
	fields := asset.Fields(components[0].Object, 10)
	if len(fields) != 1 || fields[0].Name != "m_Size" {
		t.Fatalf("fields=%#v", fields)
	}
}

func TestPrefabOverridesReportsPropertyAndRemovedComponentData(t *testing.T) {
	asset, err := ParseAsset([]byte(`%YAML 1.1
--- !u!1001 &100100000
PrefabInstance:
  m_Modification:
    m_Modifications:
    - target: {fileID: 200, guid: abcdef123456, type: 3}
      propertyPath: m_IsActive
      value: 0
      objectReference: {fileID: 0}
    m_RemovedComponents:
    - {fileID: 300, guid: abcdef123456, type: 3}
    m_AddedComponents:
    - targetCorrespondingSourceObject: {fileID: 200, guid: abcdef123456, type: 3}
      insertIndex: -1
      addedObject: {fileID: 400}
  m_SourcePrefab: {fileID: 100100000, guid: abcdef123456, type: 3}
`))
	if err != nil {
		t.Fatal(err)
	}

	overrides := asset.PrefabOverrides()
	if len(overrides) != 3 {
		t.Fatalf("overrides=%#v", overrides)
	}
	if overrides[0].Kind != "property" || overrides[0].PropertyPath != "m_IsActive" || overrides[0].Value != "0" {
		t.Fatalf("property override=%#v", overrides[0])
	}
	if overrides[1].Kind != "removed-components" || !strings.Contains(overrides[1].Target, "fileID: 300") {
		t.Fatalf("removed override=%#v", overrides[1])
	}
	if overrides[2].Kind != "added-component" || !strings.Contains(overrides[2].AddedObject, "fileID: 400") {
		t.Fatalf("added override=%#v", overrides[2])
	}
}

func TestPrefabNameOverrideLabelsStrippedGameObject(t *testing.T) {
	asset, err := ParseAsset([]byte(`%YAML 1.1
--- !u!1001 &100100000
PrefabInstance:
  m_Modification:
    m_Modifications:
    - target: {fileID: 200, guid: abcdef123456, type: 3}
      propertyPath: m_Name
      value: Ghost Root
      objectReference: {fileID: 0}
  m_SourcePrefab: {fileID: 100100000, guid: abcdef123456, type: 3}
--- !u!1 &100 stripped
GameObject:
  m_CorrespondingSourceObject: {fileID: 200, guid: abcdef123456, type: 3}
  m_PrefabInstance: {fileID: 100100000}
`))
	if err != nil {
		t.Fatal(err)
	}

	if got := asset.GameObjects()[0].Name; got != "Ghost Root" {
		t.Fatalf("name=%q", got)
	}
}

func TestQuotedUnityStringEscapesAreDecoded(t *testing.T) {
	asset, err := ParseAsset([]byte(`%YAML 1.1
--- !u!1001 &100100000
PrefabInstance:
  m_Modification:
    m_Modifications:
    - target: {fileID: 200, guid: abcdef123456, type: 3}
      propertyPath: m_Name
      value: "(Graphic) \uBBF8\uB155"
      objectReference: {fileID: 0}
  m_SourcePrefab: {fileID: 100100000, guid: abcdef123456, type: 3}
--- !u!1 &100 stripped
GameObject:
  m_CorrespondingSourceObject: {fileID: 200, guid: abcdef123456, type: 3}
  m_PrefabInstance: {fileID: 100100000}
`))
	if err != nil {
		t.Fatal(err)
	}

	if got := asset.GameObjects()[0].Name; got != "(Graphic) 미녕" {
		t.Fatalf("name=%q", got)
	}
	overrides := asset.PrefabOverrides()
	if len(overrides) != 1 || overrides[0].Value != "(Graphic) 미녕" {
		t.Fatalf("overrides=%#v", overrides)
	}
}

func TestParseAssetSummaryKeepsPrefabOverrideNames(t *testing.T) {
	asset, err := ParseAssetSummary([]byte(`%YAML 1.1
--- !u!1001 &100100000
PrefabInstance:
  m_Modification:
    m_Modifications:
    - target: {fileID: 200, guid: abcdef123456, type: 3}
      propertyPath: m_Name
      value: Ghost Root
      objectReference: {fileID: 0}
  m_SourcePrefab: {fileID: 100100000, guid: abcdef123456, type: 3}
--- !u!1 &100 stripped
GameObject:
  m_CorrespondingSourceObject: {fileID: 200, guid: abcdef123456, type: 3}
  m_PrefabInstance: {fileID: 100100000}
`))
	if err != nil {
		t.Fatal(err)
	}

	if got := asset.GameObjects()[0].Name; got != "Ghost Root" {
		t.Fatalf("name=%q", got)
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

func TestAddVisibleFieldGUIDsHonorsFieldLimitAndNestedFields(t *testing.T) {
	obj := &Object{Lines: []string{
		"  m_Script: {fileID: 11500000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}",
		"  first:",
		"    nested: {fileID: 1, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}",
		"  second: {fileID: 1, guid: cccccccccccccccccccccccccccccccc, type: 2}",
		"  third: {fileID: 1, guid: dddddddddddddddddddddddddddddddd, type: 2}",
	}}
	guids := map[string]bool{}

	AddVisibleFieldGUIDs(guids, obj, 2)
	if len(guids) != 2 ||
		!guids["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] ||
		!guids["cccccccccccccccccccccccccccccccc"] {
		t.Fatalf("guids=%#v", guids)
	}
	if guids["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"] || guids["dddddddddddddddddddddddddddddddd"] {
		t.Fatalf("hidden or script guid leaked: %#v", guids)
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

func TestFieldsResolveLocalFileIDReferences(t *testing.T) {
	asset, err := ParseAsset([]byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  m_Name: Root
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Name: Controller
  target: {fileID: 200}
`))
	if err != nil {
		t.Fatal(err)
	}

	fields := asset.Fields(asset.ByID["300"], 0)
	if len(fields) != 1 || !strings.Contains(fields[0].Value, "Transform on Root") {
		t.Fatalf("fields=%#v", fields)
	}
}

func TestFieldReferencesScansFullNestedField(t *testing.T) {
	asset, err := ParseAsset([]byte(`%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Script: {fileID: 11500000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  targets:
  - {fileID: 1, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
  - {fileID: 1, guid: cccccccccccccccccccccccccccccccc, type: 2}
  - {fileID: 1, guid: dddddddddddddddddddddddddddddddd, type: 2}
  - {fileID: 1, guid: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee, type: 2}
  - {fileID: 1, guid: ffffffffffffffffffffffffffffffff, type: 2}
`))
	if err != nil {
		t.Fatal(err)
	}

	refs := asset.FieldReferences("ffffffffffffffffffffffffffffffff")
	if len(refs) != 1 || refs[0].FieldName != "targets" {
		t.Fatalf("refs=%#v", refs)
	}
	if strings.Contains(refs[0].Value, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Fatalf("nested reference kept unrelated values: %s", refs[0].Value)
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

func TestParseAssetSummaryMatchesFullParseStructure(t *testing.T) {
	data := []byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: -300}
  m_Name: "Root Object"
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &-300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: ABCDEF1234567890ABCDEF1234567890, type: 3}
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
	full, err := ParseAsset(data)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := ParseAssetSummary(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(full.Objects) != len(summary.Objects) {
		t.Fatalf("objects full=%d summary=%d", len(full.Objects), len(summary.Objects))
	}
	for i := range full.Objects {
		f := full.Objects[i]
		s := summary.Objects[i]
		if s.Lines != nil {
			t.Fatalf("summary object %d kept lines: %#v", i, s.Lines)
		}
		if f.ID != s.ID || f.ClassID != s.ClassID || f.Type != s.Type || f.Order != s.Order ||
			f.Name != s.Name || f.GameObjectID != s.GameObjectID ||
			f.FatherTransformID != s.FatherTransformID || f.ScriptGUID != s.ScriptGUID ||
			!reflect.DeepEqual(f.ComponentIDs, s.ComponentIDs) {
			t.Fatalf("object %d differs\nfull=%#v\nsummary=%#v", i, f, s)
		}
	}
	if got, want := hierarchyPaths(summary), []string{"Root Object", "Root Object/Child"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths=%#v want=%#v", got, want)
	}
}

func TestParseAssetSummaryMatchesFullParseStructureVariants(t *testing.T) {
	data := []byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: -300}
  m_Name: ""
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: -400}
--- !u!224 &-400
RectTransform:
  m_GameObject: {fileID: 101}
  m_Father: {fileID: 0}
--- !u!114 &-300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: FEDCBA654321FEDCBA654321FEDCBA65, type: 3}
--- !u!1 &101
GameObject:
  m_Component:
  - component: {fileID: -400}
  m_Name: "UI Root"
`)
	full, err := ParseAsset(data)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := ParseAssetSummary(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(full.Objects) != len(summary.Objects) {
		t.Fatalf("objects full=%d summary=%d", len(full.Objects), len(summary.Objects))
	}
	for i := range full.Objects {
		f := full.Objects[i]
		s := summary.Objects[i]
		if f.ID != s.ID || f.ClassID != s.ClassID || f.Type != s.Type ||
			f.Name != s.Name || f.GameObjectID != s.GameObjectID ||
			f.FatherTransformID != s.FatherTransformID || f.ScriptGUID != s.ScriptGUID ||
			!reflect.DeepEqual(f.ComponentIDs, s.ComponentIDs) {
			t.Fatalf("object %d differs\nfull=%#v\nsummary=%#v", i, f, s)
		}
	}
}

func TestReadAssetSummarySkipsMetaGUID(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	prefab := filepath.Join(assets, "Foo.prefab")
	data := []byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Name: Root
`)
	if err := os.WriteFile(prefab, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefab+".meta", []byte("guid: abcdef123456\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := FileEntry{Abs: prefab, AssetPath: "Assets/Foo.prefab", Kind: "prefab"}

	full, err := ReadAsset(entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := ReadAssetSummary(entry, nil)
	if err != nil {
		t.Fatal(err)
	}

	if full.GUID != "abcdef123456" {
		t.Fatalf("full GUID=%q", full.GUID)
	}
	if summary.GUID != "" {
		t.Fatalf("summary GUID=%q", summary.GUID)
	}
	if summary.Path != "Assets/Foo.prefab" || summary.Kind != "prefab" || len(summary.Objects) != 1 {
		t.Fatalf("summary=%#v", summary)
	}
}

func TestExtractUnityYAMLReferences(t *testing.T) {
	if classID, id, ok := parseHeaderLine([]byte("--- !u!114 &-300")); !ok || classID != 114 || id != "-300" {
		t.Fatalf("header classID=%d id=%q ok=%v", classID, id, ok)
	}
	if got := extractFileID("  m_GameObject: {fileID: -123, guid: abc}"); got != "-123" {
		t.Fatalf("fileID=%q", got)
	}
	if got := extractGUID("  ref: {fileID: 1, guid: ABCDEF1234567890, type: 3}"); got != "abcdef1234567890" {
		t.Fatalf("guid=%q", got)
	}
	if got := findGUIDs("a {guid: ABCDEF} b {guid: 123456} c"); !reflect.DeepEqual(got, []string{"abcdef", "123456"}) {
		t.Fatalf("guids=%#v", got)
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

func hierarchyPaths(asset *Asset) []string {
	var paths []string
	for _, node := range asset.FlattenNodes() {
		paths = append(paths, node.Path)
	}
	return paths
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

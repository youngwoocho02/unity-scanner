package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

func TestHierarchyRowClassification(t *testing.T) {
	row := hierarchyRow{Components: []string{"Transform", "MeshFilter", "MeshRenderer"}}
	if !isRenderOnly(row.Components) {
		t.Fatalf("expected render-only row")
	}
	if hasFocusComponent(row.Components) {
		t.Fatalf("render-only row should not be focus")
	}
	if isRenderOnly([]string{"Transform"}) {
		t.Fatalf("transform-only row should not be treated as render-only")
	}

	row = hierarchyRow{Components: []string{"Transform", "Camera"}}
	if !hasFocusComponent(row.Components) {
		t.Fatalf("camera row should be focus")
	}
}

func TestLimitFocusRowsIgnoresTreeLimitFillers(t *testing.T) {
	rows := []hierarchyRow{
		{Index: 0},
		{Index: 1},
		{Index: 2},
		{Index: 3, Focus: true},
	}

	focus, hidden := limitFocusRows(rows, 2)
	if hidden != 0 || len(focus) != 1 || focus[0].Index != 3 {
		t.Fatalf("focus=%#v hidden=%d", focus, hidden)
	}
	tree, hidden := limitTreeRows(rows, 2)
	if hidden != 2 || len(tree) != 2 {
		t.Fatalf("tree=%#v hidden=%d", tree, hidden)
	}
}

func TestCollectHierarchyRowsDoesNotApplyDisplayLimit(t *testing.T) {
	asset, err := unityasset.ParseAsset([]byte(`%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  m_Name: FillerA
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!1 &101
GameObject:
  m_Component:
  - component: {fileID: 201}
  m_Name: FillerB
--- !u!4 &201
Transform:
  m_GameObject: {fileID: 101}
  m_Father: {fileID: 0}
--- !u!1 &102
GameObject:
  m_Component:
  - component: {fileID: 202}
  - component: {fileID: 300}
  m_Name: Focus
--- !u!4 &202
Transform:
  m_GameObject: {fileID: 102}
  m_Father: {fileID: 0}
--- !u!20 &300
Camera:
  m_GameObject: {fileID: 102}
`))
	if err != nil {
		t.Fatal(err)
	}

	roots := asset.Hierarchy()
	rows, hidden := collectHierarchyRows(asset, roots, buildReadComponentView(asset, flattenHierarchy(roots)), readOptions{depth: 0, limit: 2})
	if hidden != 0 || len(rows) != 3 {
		t.Fatalf("rows=%d hidden=%d", len(rows), hidden)
	}
	focus, _ := limitFocusRows(rows, 2)
	if len(focus) != 1 || focus[0].Node.GameObject.Name != "Focus" {
		t.Fatalf("focus=%#v", focus)
	}
}

func TestCollapsedRunRequiresSameDepthAndComponentSet(t *testing.T) {
	rows := []hierarchyRow{
		{Node: &unityasset.Node{Depth: 1}, ComponentSet: "c1", RenderOnly: true},
		{Node: &unityasset.Node{Depth: 1}, ComponentSet: "c1", RenderOnly: true},
		{Node: &unityasset.Node{Depth: 2}, ComponentSet: "c1", RenderOnly: true},
	}

	if got := collapsibleRunEnd(rows, 0); got != 2 {
		t.Fatalf("run end=%d", got)
	}
}

func TestFieldReferenceGUIDsFiltersComponentAndSkipsScript(t *testing.T) {
	asset, err := unityasset.ParseAsset([]byte(`%YAML 1.1
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
  m_Script: {fileID: 11500000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  target: {fileID: 1, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
--- !u!114 &301
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: cccccccccccccccccccccccccccccccc, type: 3}
  target: {fileID: 1, guid: dddddddddddddddddddddddddddddddd, type: 2}
`))
	if err != nil {
		t.Fatal(err)
	}
	asset.Kind = "prefab"
	asset.ScriptIndex = unityasset.ScriptIndex{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "Assets/Scripts/TargetComponent.cs",
		"cccccccccccccccccccccccccccccccc": "Assets/Scripts/OtherComponent.cs",
	}

	nodes := asset.FlattenNodes()
	guids := fieldReferenceGUIDs(asset, nodes, buildReadComponentView(asset, nodes), readOptions{component: "TargetComponent"})
	if len(guids) != 1 || !guids["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] {
		t.Fatalf("guids=%#v", guids)
	}
}

func TestFieldReferenceGUIDsHonorsFieldLimit(t *testing.T) {
	asset, err := unityasset.ParseAsset([]byte(`%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Script: {fileID: 11500000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  first: {fileID: 1, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
  second: {fileID: 1, guid: cccccccccccccccccccccccccccccccc, type: 2}
`))
	if err != nil {
		t.Fatal(err)
	}
	asset.Kind = "asset"

	guids := fieldReferenceGUIDs(asset, nil, readComponentView{}, readOptions{fieldLimit: 1})
	if len(guids) != 1 || !guids["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] {
		t.Fatalf("guids=%#v", guids)
	}
}

func TestReadCmdResolvesFieldReferencesWithTargetedGUIDIndex(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	writeTestFile(t, filepath.Join(assets, "Scripts", "ConfigAsset.cs.meta"), "guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	writeTestFile(t, filepath.Join(assets, "Data", "Target.asset"), "x")
	writeTestFile(t, filepath.Join(assets, "Data", "Target.asset.meta"), "guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n")
	writeTestFile(t, filepath.Join(assets, "Config.asset.meta"), "guid: cccccccccccccccccccccccccccccccc\n")
	writeTestFile(t, filepath.Join(assets, "Config.asset"), `%YAML 1.1
--- !u!114 &11400000
MonoBehaviour:
  m_Script: {fileID: 11500000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  m_Name: Config
  target: {fileID: 1, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
`)

	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	err := readCmd([]string{"-p", dir, "Assets/Config.asset", "--field-limit", "10"})
	restoreStdout()
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"ASSET       asset",
		"script: Assets/Scripts/ConfigAsset.cs",
		"target                   {fileID: 1, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2} -> Assets/Data/Target.asset",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ->") {
		t.Fatalf("script guid was resolved as field ref:\n%s", out)
	}
}

func TestReadCmdReportsComponentFieldsOnlyForMatchingComponent(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "Assets")
	writeTestFile(t, filepath.Join(assets, "Scripts", "TargetComponent.cs.meta"), "guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	writeTestFile(t, filepath.Join(assets, "Scripts", "OtherComponent.cs.meta"), "guid: cccccccccccccccccccccccccccccccc\n")
	writeTestFile(t, filepath.Join(assets, "Data", "Target.asset"), "x")
	writeTestFile(t, filepath.Join(assets, "Data", "Target.asset.meta"), "guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n")
	writeTestFile(t, filepath.Join(assets, "Data", "Other.asset"), "x")
	writeTestFile(t, filepath.Join(assets, "Data", "Other.asset.meta"), "guid: dddddddddddddddddddddddddddddddd\n")
	writeTestFile(t, filepath.Join(assets, "Item.prefab.meta"), "guid: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee\n")
	writeTestFile(t, filepath.Join(assets, "Item.prefab"), `%YAML 1.1
--- !u!1 &100
GameObject:
  m_Component:
  - component: {fileID: 200}
  - component: {fileID: 300}
  - component: {fileID: 301}
  m_Name: Root
--- !u!4 &200
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!114 &300
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  target: {fileID: 1, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
--- !u!114 &301
MonoBehaviour:
  m_GameObject: {fileID: 100}
  m_Script: {fileID: 11500000, guid: cccccccccccccccccccccccccccccccc, type: 3}
  other: {fileID: 1, guid: dddddddddddddddddddddddddddddddd, type: 2}
`)

	var buf bytes.Buffer
	restoreStdout := captureStdout(&buf)
	err := readCmd([]string{"-p", dir, "Assets/Item.prefab", "--component", "TargetComponent", "--field-limit", "10"})
	restoreStdout()
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "COMPONENT  TargetComponent") ||
		!strings.Contains(out, "-> Assets/Data/Target.asset") {
		t.Fatalf("target component not resolved:\n%s", out)
	}
	if strings.Contains(out, "OtherComponent") || strings.Contains(out, "Assets/Data/Other.asset") {
		t.Fatalf("non-matching component leaked:\n%s", out)
	}
}

func BenchmarkReadHierarchyRowsManyComponents(b *testing.B) {
	asset, err := unityasset.ParseAsset([]byte(readBenchmarkPrefabYAML(1000)))
	if err != nil {
		b.Fatal(err)
	}
	roots := asset.Hierarchy()
	flat := flattenHierarchy(roots)
	components := buildReadComponentView(asset, flat)
	opts := readOptions{depth: 999, limit: 2000}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rows, hidden := collectHierarchyRows(asset, roots, components, opts)
		if len(rows) != 1000 || hidden != 0 {
			b.Fatalf("rows=%d hidden=%d", len(rows), hidden)
		}
	}
}

func readBenchmarkPrefabYAML(count int) string {
	var b strings.Builder
	b.WriteString("%YAML 1.1\n")
	for i := 0; i < count; i++ {
		goID := 100000 + i
		transformID := 200000 + i
		rendererID := 300000 + i
		b.WriteString(fmt.Sprintf("--- !u!1 &%d\nGameObject:\n", goID))
		b.WriteString(fmt.Sprintf("  m_Component:\n  - component: {fileID: %d}\n  - component: {fileID: %d}\n", transformID, rendererID))
		b.WriteString(fmt.Sprintf("  m_Name: Mesh_%04d\n", i))
		b.WriteString(fmt.Sprintf("--- !u!4 &%d\nTransform:\n", transformID))
		b.WriteString(fmt.Sprintf("  m_GameObject: {fileID: %d}\n  m_Father: {fileID: 0}\n", goID))
		b.WriteString(fmt.Sprintf("--- !u!23 &%d\nMeshRenderer:\n", rendererID))
		b.WriteString(fmt.Sprintf("  m_GameObject: {fileID: %d}\n", goID))
	}
	return b.String()
}

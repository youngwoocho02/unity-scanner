package cmd

import (
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

	rows, hidden := collectHierarchyRows(asset, asset.Hierarchy(), readOptions{depth: 0, limit: 2})
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

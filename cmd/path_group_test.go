package cmd

import "testing"

func TestGroupPathsCompactsSharedDirectories(t *testing.T) {
	groups := groupPaths([]string{
		"Assets/Prefabs/Cook/A.prefab",
		"Assets/Prefabs/Cook/B.prefab",
		"Assets/Prefabs/UI/Panel.prefab",
	}, "")
	if len(groups) != 2 {
		t.Fatalf("groups=%#v", groups)
	}
	if groups[0].Dir != "Prefabs/Cook" || len(groups[0].Names) != 2 || groups[0].Names[0] != "A" || groups[0].Names[1] != "B" {
		t.Fatalf("first group=%#v", groups[0])
	}
	if groups[1].Dir != "Prefabs/UI" || len(groups[1].Names) != 1 || groups[1].Names[0] != "Panel" {
		t.Fatalf("second group=%#v", groups[1])
	}
}

func TestCommonPathRoot(t *testing.T) {
	root := commonPathRoot([]string{
		"Assets/Accelix/Prefabs/Cook/A.prefab",
		"Assets/Accelix/Graphic/UI/B.prefab",
	})
	if root != "Assets/Accelix" {
		t.Fatalf("root=%q", root)
	}
}

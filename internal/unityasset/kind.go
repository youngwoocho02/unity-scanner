package unityasset

import (
	"path/filepath"
	"sort"
	"strings"
)

var extToKind = map[string]string{
	".anim":               "anim",
	".asset":              "asset",
	".controller":         "controller",
	".cs":                 "cs",
	".mat":                "mat",
	".overrideController": "controller",
	".physicsMaterial2D":  "physics2d",
	".physicMaterial":     "physics",
	".playable":           "playable",
	".prefab":             "prefab",
	".shader":             "shader",
	".spriteatlas":        "atlas",
	".unity":              "scene",
	".uss":                "uss",
	".uxml":               "uxml",
	".meta":               "meta",
}

var kindToExt = map[string]string{
	"anim":       ".anim",
	"asset":      ".asset",
	"atlas":      ".spriteatlas",
	"controller": ".controller",
	"cs":         ".cs",
	"mat":        ".mat",
	"physics":    ".physicMaterial",
	"physics2d":  ".physicsMaterial2D",
	"playable":   ".playable",
	"prefab":     ".prefab",
	"scene":      ".unity",
	"shader":     ".shader",
	"uss":        ".uss",
	"uxml":       ".uxml",
}

var kindAliases = map[string]string{
	"prefabs":   "prefab",
	"scenes":    "scene",
	"unity":     "scene",
	"scripts":   "cs",
	"script":    "cs",
	"material":  "mat",
	"materials": "mat",
}

func KindForPath(path string) string {
	ext := filepath.Ext(path)
	if kind, ok := extToKind[ext]; ok {
		return kind
	}
	if ext == "" {
		return "none"
	}
	return strings.TrimPrefix(strings.ToLower(ext), ".")
}

func ExtForKind(kind string) string {
	kind = NormalizeKind(kind)
	if ext := kindToExt[kind]; ext != "" {
		return ext
	}
	return "." + kind
}

func NeedsExtDeclaration(kind string) bool {
	kind = NormalizeKind(kind)
	if _, ok := kindToExt[kind]; ok {
		return true
	}
	ext := ExtForKind(kind)
	return strings.TrimPrefix(ext, ".") != kind
}

func NormalizeKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if alias, ok := kindAliases[kind]; ok {
		return alias
	}
	return kind
}

func ParseKindSet(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		kind := NormalizeKind(part)
		if kind != "" {
			out[kind] = true
		}
	}
	return out
}

func KnownUnityYAMLKind(kind string) bool {
	switch NormalizeKind(kind) {
	case "prefab", "scene", "asset", "mat", "controller", "anim", "playable":
		return true
	default:
		return false
	}
}

func SortedKinds(counts map[string]int) []string {
	kinds := make([]string, 0, len(counts))
	for kind, count := range counts {
		if count > 0 && kind != "meta" {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	return kinds
}

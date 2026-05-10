package unityasset

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileEntry struct {
	Abs       string
	AssetPath string
	Dir       string
	Name      string
	Ext       string
	Kind      string
	IsMeta    bool
}

type ScanOptions struct {
	Kinds       map[string]bool
	IncludeMeta bool
}

type ScanResult struct {
	Files     []FileEntry
	MetaCount int
	KindCount map[string]int
}

func Scan(p Project, input string, opts ScanOptions) (ScanResult, error) {
	abs, _, err := p.Resolve(input)
	if err != nil {
		return ScanResult{}, err
	}

	result := ScanResult{KindCount: map[string]int{}}
	info, err := os.Stat(abs)
	if err != nil {
		return ScanResult{}, err
	}

	visit := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != abs {
				return filepath.SkipDir
			}
			return nil
		}

		entry := makeEntry(p, path)
		if entry.IsMeta {
			result.MetaCount++
			if !opts.IncludeMeta {
				return nil
			}
		}
		if len(opts.Kinds) > 0 && !opts.Kinds[entry.Kind] {
			return nil
		}
		result.KindCount[entry.Kind]++
		result.Files = append(result.Files, entry)
		return nil
	}

	if !info.IsDir() {
		entry := makeEntry(p, abs)
		if entry.IsMeta {
			result.MetaCount = 1
		}
		if acceptEntry(entry, opts) {
			result.KindCount[entry.Kind]++
			result.Files = append(result.Files, entry)
		}
		return result, nil
	}

	err = filepath.WalkDir(abs, visit)
	if err != nil {
		return ScanResult{}, err
	}

	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].AssetPath < result.Files[j].AssetPath
	})
	return result, nil
}

func acceptEntry(entry FileEntry, opts ScanOptions) bool {
	if entry.IsMeta && !opts.IncludeMeta {
		return false
	}
	if len(opts.Kinds) > 0 && !opts.Kinds[entry.Kind] {
		return false
	}
	return true
}

func makeEntry(p Project, abs string) FileEntry {
	assetPath := p.AssetPath(abs)
	ext := filepath.Ext(abs)
	name := strings.TrimSuffix(filepath.Base(abs), ext)
	kind := KindForPath(abs)
	return FileEntry{
		Abs:       abs,
		AssetPath: assetPath,
		Dir:       filepath.ToSlash(filepath.Dir(assetPath)),
		Name:      name,
		Ext:       ext,
		Kind:      kind,
		IsMeta:    kind == "meta",
	}
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".vs", "Library", "Logs", "obj", "Obj", "Temp", "Build", "Builds", "UserSettings":
		return true
	default:
		return false
	}
}

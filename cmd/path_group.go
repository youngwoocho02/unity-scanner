package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/youngwoocho02/unity-scanner/internal/format"
)

type pathNameGroup struct {
	Dir   string
	Names []string
}

func groupPaths(paths []string, rootPath string) []pathNameGroup {
	byDir := map[string][]string{}
	for _, path := range paths {
		path = filepath.ToSlash(path)
		dir := compactGroupDir(filepath.ToSlash(filepath.Dir(path)), rootPath)
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		byDir[dir] = append(byDir[dir], name)
	}

	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	groups := make([]pathNameGroup, 0, len(dirs))
	for _, dir := range dirs {
		groups = append(groups, pathNameGroup{Dir: dir, Names: format.CompressNames(byDir[dir])})
	}
	return groups
}

func printPathGroups(paths []string, rootPath string, lineWidth int) {
	for _, group := range groupPaths(paths, rootPath) {
		for _, line := range format.Lines(group.Names, 6) {
			printfLineLimited(lineWidth, "  %s :: %s", group.Dir, line)
		}
	}
}

func printPathGroupSection(label string, paths []string, lineWidth int) {
	root := commonPathRoot(paths)
	if root == "" {
		fmt.Println(label)
	} else {
		fmt.Printf("%s %s\n", label, root)
	}
	printPathGroups(paths, root, lineWidth)
}

func commonPathRoot(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	common := strings.Split(strings.Trim(filepath.ToSlash(filepath.Dir(paths[0])), "/"), "/")
	for _, path := range paths[1:] {
		parts := strings.Split(strings.Trim(filepath.ToSlash(filepath.Dir(path)), "/"), "/")
		limit := len(common)
		if len(parts) < limit {
			limit = len(parts)
		}
		i := 0
		for i < limit && common[i] == parts[i] {
			i++
		}
		common = common[:i]
	}
	if len(common) == 0 {
		return ""
	}
	return strings.Join(common, "/")
}

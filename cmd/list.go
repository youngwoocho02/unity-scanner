package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/youngwoocho02/unity-scanner/internal/format"
	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

type listOptions struct {
	commonOptions
	depth       int
	kind        string
	includeMeta bool
	flat        bool
	limit       int
}

func listCmd(args []string) error {
	opts := listOptions{depth: 2, limit: 80}
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	addCommonFlags(fs, &opts.commonOptions)
	fs.IntVar(&opts.depth, "depth", opts.depth, "directory summary depth")
	fs.StringVar(&opts.kind, "kind", "", "comma-separated kind filter")
	fs.BoolVar(&opts.includeMeta, "meta", false, "include .meta files")
	fs.BoolVar(&opts.flat, "flat", false, "omit directory summary")
	fs.IntVar(&opts.limit, "limit", opts.limit, "max groups")
	if err := parse(fs, args); err != nil {
		if err == flag.ErrHelp {
			printTopicHelp(os.Stdout, "list")
			return nil
		}
		return err
	}

	target := "Assets"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	project, err := unityasset.OpenProject(opts.project)
	if err != nil {
		return err
	}
	result, err := unityasset.Scan(project, target, unityasset.ScanOptions{
		Kinds:       unityasset.ParseKindSet(opts.kind),
		IncludeMeta: opts.includeMeta,
	})
	if err != nil {
		return err
	}
	_, rootPath, _ := project.Resolve(target)
	printList(rootPath, result, opts)
	return nil
}

func printList(rootPath string, result unityasset.ScanResult, opts listOptions) {
	groups := groupEntries(result.Files)

	printExt(result.KindCount)
	if !opts.flat {
		printDirs(result.Files, rootPath, opts.depth)
	}
	printGroups(groups, rootPath, opts.limit)
}

func printExt(counts map[string]int) {
	kinds := unityasset.SortedKinds(counts)
	if len(kinds) == 0 {
		return
	}
	fmt.Println("EXT")
	omitted := 0
	for _, kind := range kinds {
		if !unityasset.NeedsExtDeclaration(kind) {
			omitted++
			continue
		}
		fmt.Printf("  %-10s %s\n", kind, unityasset.ExtForKind(kind))
	}
	if omitted > 0 {
		fmt.Printf("  other      %d direct .kind mappings omitted\n", omitted)
	}
	fmt.Println()
}

type dirCounts map[string]map[string]int

func printDirs(files []unityasset.FileEntry, root string, depth int) {
	if depth < 0 || len(files) == 0 {
		return
	}
	counts := dirCounts{}
	for _, file := range files {
		dir := trimDirToDepth(file.Dir, root, depth)
		if counts[dir] == nil {
			counts[dir] = map[string]int{}
		}
		counts[dir][file.Kind]++
	}

	dirs := make([]string, 0, len(counts))
	for dir := range counts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	labels := make(map[string]string, len(dirs))
	width := 0
	for _, dir := range dirs {
		label := "./"
		if dir != "." {
			label = dir + "/"
		}
		labels[dir] = label
		if len(label) > width {
			width = len(label)
		}
	}

	fmt.Println("DIRS")
	for _, dir := range dirs {
		fmt.Printf("  %-*s %s\n", width, labels[dir], kindCounts(counts[dir]))
	}
	fmt.Println()
}

func trimDirToDepth(dir, root string, depth int) string {
	dir = strings.TrimPrefix(dir, root)
	dir = strings.Trim(dir, "/")
	if dir == "" || dir == "." {
		return "."
	}
	parts := strings.Split(dir, "/")
	if len(parts) > depth {
		parts = parts[:depth]
	}
	return strings.Join(parts, "/")
}

func kindCounts(counts map[string]int) string {
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, fmt.Sprintf("%s %d", kind, counts[kind]))
	}
	return strings.Join(parts, ", ")
}

type entryGroup struct {
	Dir   string
	Kind  string
	Names []string
}

func groupEntries(files []unityasset.FileEntry) []entryGroup {
	byKey := map[string]*entryGroup{}
	for _, file := range files {
		key := file.Dir + "\x00" + file.Kind
		group := byKey[key]
		if group == nil {
			group = &entryGroup{Dir: file.Dir, Kind: file.Kind}
			byKey[key] = group
		}
		name := file.Name
		if file.Kind == "meta" {
			name = strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
		}
		group.Names = append(group.Names, name)
	}

	groups := make([]entryGroup, 0, len(byKey))
	for _, group := range byKey {
		group.Names = format.CompressNames(group.Names)
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Dir != groups[j].Dir {
			return groups[i].Dir < groups[j].Dir
		}
		return groups[i].Kind < groups[j].Kind
	})
	return groups
}

func printGroups(groups []entryGroup, rootPath string, limit int) {
	if len(groups) == 0 {
		fmt.Println("GROUPS\n  <empty>")
		return
	}
	if limit <= 0 || limit > len(groups) {
		limit = len(groups)
	}

	fmt.Println("GROUPS")
	for i := 0; i < limit; i++ {
		group := groups[i]
		fmt.Printf("  %s  [%s]\n", compactGroupDir(group.Dir, rootPath), group.Kind)
		for _, line := range format.Lines(group.Names, 6) {
			fmt.Printf("    %s\n", line)
		}
	}
	if len(groups) > limit {
		fmt.Printf("\nmore groups: %d hidden by --limit\n", len(groups)-limit)
	}
}

func compactGroupDir(dir, rootPath string) string {
	dir = strings.Trim(dir, "/")
	rootPath = strings.Trim(rootPath, "/")
	if rootPath != "" && dir == rootPath {
		return "."
	}
	if rootPath != "" {
		dir = strings.TrimPrefix(dir, rootPath+"/")
	}
	dir = strings.TrimPrefix(dir, "Assets/")
	if dir == "Assets" || dir == "" {
		return "."
	}
	return dir
}

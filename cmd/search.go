package cmd

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/youngwoocho02/unity-scanner/internal/format"
	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

type searchOptions struct {
	commonOptions
	name      string
	component string
	guid      string
	ref       string
	types     string
	compact   bool
	limit     int
	rootPath  string
}

type searchMatch struct {
	File        unityasset.FileEntry
	Objects     []objectMatch
	RawGUID     bool
	FileNameHit bool
}

type searchWarning struct {
	Path string
	Err  error
}

type objectMatch struct {
	Path       string
	Components []string
}

type searchFileResult struct {
	Index    int
	Match    searchMatch
	Matched  bool
	Warnings []searchWarning
}

const textSearchChunkSize = 1024 * 1024

func searchCmd(args []string) error {
	opts := searchOptions{limit: 80}
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	addCommonFlags(fs, &opts.commonOptions)
	fs.StringVar(&opts.name, "name", "", "file or GameObject name")
	fs.StringVar(&opts.component, "component", "", "component or script name")
	fs.StringVar(&opts.guid, "guid", "", "raw Unity GUID")
	fs.StringVar(&opts.ref, "ref", "", "raw Unity GUID alias")
	fs.StringVar(&opts.types, "type", "", "comma-separated asset kinds")
	fs.BoolVar(&opts.compact, "compact", false, "compact output")
	fs.IntVar(&opts.limit, "limit", opts.limit, "max result files")
	if err := parse(fs, args); err != nil {
		if err == flag.ErrHelp {
			printTopicHelp(os.Stdout, "search")
			return nil
		}
		return err
	}
	if opts.guid == "" {
		opts.guid = opts.ref
	}
	if opts.name == "" && opts.component == "" && opts.guid == "" {
		return fmt.Errorf("search requires --name, --component, --guid, or --ref")
	}

	target := "Assets"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	project, err := unityasset.OpenProject(opts.project)
	if err != nil {
		return err
	}
	kinds := unityasset.ParseKindSet(opts.types)
	kinds = defaultSearchKinds(kinds, opts)
	result, err := unityasset.Scan(project, target, unityasset.ScanOptions{
		Kinds: kinds,
	})
	if err != nil {
		return err
	}

	scripts := unityasset.ScriptIndex{}
	if opts.component != "" {
		scripts, err = unityasset.BuildScriptIndex(project)
		if err != nil {
			return err
		}
	}

	_, opts.rootPath, _ = project.Resolve(target)
	matches, warnings := runSearch(project, result.Files, scripts, opts)
	printSearch(matches, result.KindCount, opts, warnings)
	return nil
}

func defaultSearchKinds(kinds map[string]bool, opts searchOptions) map[string]bool {
	if len(kinds) > 0 || (opts.guid == "" && opts.component == "") {
		return kinds
	}
	return unityasset.ParseKindSet("prefab,scene,asset,mat,controller")
}

func runSearch(project unityasset.Project, files []unityasset.FileEntry, scripts unityasset.ScriptIndex, opts searchOptions) ([]searchMatch, []searchWarning) {
	if len(files) < 32 {
		return runSearchSerial(project, files, scripts, opts)
	}

	workers := runtime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan int)
	results := make(chan searchFileResult, len(files))

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results <- searchOneFile(project, index, files[index], scripts, opts)
			}
		}()
	}

	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(results)

	collected := make([]searchFileResult, len(files))
	for result := range results {
		collected[result.Index] = result
	}

	matches := make([]searchMatch, 0)
	warnings := make([]searchWarning, 0)
	for _, result := range collected {
		warnings = append(warnings, result.Warnings...)
		if result.Matched {
			matches = append(matches, result.Match)
		}
	}
	return matches, warnings
}

func runSearchSerial(project unityasset.Project, files []unityasset.FileEntry, scripts unityasset.ScriptIndex, opts searchOptions) ([]searchMatch, []searchWarning) {
	matches := make([]searchMatch, 0)
	warnings := make([]searchWarning, 0)
	for i, file := range files {
		result := searchOneFile(project, i, file, scripts, opts)
		warnings = append(warnings, result.Warnings...)
		if result.Matched {
			matches = append(matches, result.Match)
		}
	}
	return matches, warnings
}

func searchOneFile(project unityasset.Project, index int, file unityasset.FileEntry, scripts unityasset.ScriptIndex, opts searchOptions) searchFileResult {
	result := searchFileResult{Index: index, Match: searchMatch{File: file}}
	if file.IsMeta {
		return result
	}

	if opts.guid != "" {
		ok, err := fileContains(file, opts.guid)
		if err != nil {
			result.Warnings = append(result.Warnings, searchWarning{Path: file.AssetPath, Err: err})
		}
		result.Match.RawGUID = ok
	}
	if opts.name != "" && opts.component == "" && containsFold(file.Name, opts.name) {
		result.Match.FileNameHit = true
	}

	needsStructured := opts.name != "" || opts.component != ""
	if needsStructured && unityasset.KnownUnityYAMLKind(file.Kind) {
		asset, err := unityasset.ReadAsset(project, file, scripts)
		if err == nil {
			result.Match.Objects = objectMatches(asset, opts)
		} else {
			result.Warnings = append(result.Warnings, searchWarning{Path: file.AssetPath, Err: err})
		}
	}

	result.Matched = result.Match.RawGUID || result.Match.FileNameHit || len(result.Match.Objects) > 0
	return result
}

func objectMatches(asset *unityasset.Asset, opts searchOptions) []objectMatch {
	var out []objectMatch
	for _, node := range asset.FlattenNodes() {
		nameOK := opts.name == "" || containsFold(node.GameObject.Name, opts.name)
		components := asset.ComponentsFor(node.GameObject.ID)
		componentNames := make([]string, 0, len(components))
		componentOK := opts.component == ""
		for _, component := range components {
			componentNames = append(componentNames, component.Name)
			if containsFold(component.Name, opts.component) {
				componentOK = true
			}
		}
		if nameOK && componentOK {
			out = append(out, objectMatch{Path: node.Path, Components: componentNames})
		}
	}
	if len(asset.GameObjects()) == 0 {
		for _, obj := range asset.Objects {
			name, scriptPath := asset.ComponentName(obj)
			nameOK := opts.name == "" || containsFold(obj.Name, opts.name) || containsFold(name, opts.name) || containsFold(scriptPath, opts.name)
			componentOK := opts.component == "" || containsFold(name, opts.component) || containsFold(scriptPath, opts.component)
			if nameOK && componentOK {
				label := obj.Name
				if label == "" {
					label = name
				}
				out = append(out, objectMatch{Path: label, Components: []string{name}})
			}
		}
	}
	return out
}

func printSearch(matches []searchMatch, kindCounts map[string]int, opts searchOptions, warnings []searchWarning) {
	if opts.limit <= 0 || opts.limit > len(matches) {
		opts.limit = len(matches)
	}

	printSearchExt(matches, kindCounts)
	fmt.Printf("MATCHES  %d", len(matches))
	if opts.limit < len(matches) {
		fmt.Printf(" shown %d", opts.limit)
	}
	fmt.Print("\n\n")

	if opts.compact {
		printSearchCompact(matches[:opts.limit], opts.rootPath)
		printSearchWarnings(warnings)
		return
	}

	printSearchDetailed(matches[:opts.limit], opts)
	if len(matches) > opts.limit {
		fmt.Printf("\nmore: %d hidden by --limit\n", len(matches)-opts.limit)
	}
	printSearchWarnings(warnings)
}

func printSearchDetailed(matches []searchMatch, opts searchOptions) {
	for _, group := range groupSearchMatches(matches) {
		fmt.Printf("[%s] %s\n", group.Kind, compactGroupDir(group.Dir, opts.rootPath))
		for _, match := range group.Matches {
			printSearchMatch(match)
		}
	}
}

type searchMatchGroup struct {
	Kind    string
	Dir     string
	Matches []searchMatch
}

func groupSearchMatches(matches []searchMatch) []searchMatchGroup {
	index := map[string]int{}
	groups := make([]searchMatchGroup, 0)
	for _, match := range matches {
		key := match.File.Kind + "\x00" + match.File.Dir
		groupIndex, ok := index[key]
		if !ok {
			groupIndex = len(groups)
			index[key] = groupIndex
			groups = append(groups, searchMatchGroup{Kind: match.File.Kind, Dir: match.File.Dir})
		}
		groups[groupIndex].Matches = append(groups[groupIndex].Matches, match)
	}
	return groups
}

func printSearchMatch(match searchMatch) {
	fmt.Printf("  %s\n", match.File.Name)
	if match.FileNameHit {
		fmt.Println("    file-name")
	}
	if match.RawGUID {
		fmt.Println("    guid-reference")
	}
	objectLimit := len(match.Objects)
	if objectLimit > 12 {
		objectLimit = 12
	}
	for _, obj := range match.Objects[:objectLimit] {
		fmt.Printf("    object: %s\n", obj.Path)
		if len(obj.Components) > 0 {
			fmt.Printf("    components: %s\n", strings.Join(obj.Components, ", "))
		}
	}
	if len(match.Objects) > objectLimit {
		fmt.Printf("    more objects: %d hidden\n", len(match.Objects)-objectLimit)
	}
}

func printSearchExt(matches []searchMatch, kindCounts map[string]int) {
	counts := map[string]int{}
	for _, match := range matches {
		counts[match.File.Kind]++
	}
	if len(counts) == 0 {
		counts = kindCounts
	}
	printExt(counts)
}

func printSearchCompact(matches []searchMatch, rootPath string) {
	grouped := map[string][]string{}
	for _, match := range matches {
		key := match.File.Kind + "\x00" + match.File.Dir
		grouped[key] = append(grouped[key], match.File.Name)
	}

	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		names := format.CompressNames(grouped[key])
		fmt.Printf("[%s]\n", parts[0])
		for _, line := range format.Lines(names, 8) {
			fmt.Printf("  %s :: %s\n", compactGroupDir(parts[1], rootPath), line)
		}
	}
}

func printSearchWarnings(warnings []searchWarning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Printf("\nwarnings: %d files skipped or failed\n", len(warnings))
	limit := len(warnings)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		fmt.Printf("  %s: %v\n", warnings[i].Path, warnings[i].Err)
	}
	if len(warnings) > limit {
		fmt.Printf("  more warnings: %d hidden\n", len(warnings)-limit)
	}
}

func fileContains(file unityasset.FileEntry, needle string) (bool, error) {
	if !isTextSearchKind(file.Kind) {
		return false, nil
	}
	f, err := os.Open(file.Abs)
	if err != nil {
		return false, err
	}
	defer f.Close()

	needleBytes := []byte(needle)
	if len(needleBytes) == 0 {
		return true, nil
	}

	overlap := len(needleBytes) - 1
	buf := make([]byte, textSearchBufferSize(f))
	tail := make([]byte, 0, overlap)
	bridge := make([]byte, 0, overlap*2)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if len(tail) > 0 {
				bridge = append(bridge[:0], tail...)
				take := overlap
				if take > len(chunk) {
					take = len(chunk)
				}
				bridge = append(bridge, chunk[:take]...)
				if bytes.Contains(bridge, needleBytes) {
					return true, nil
				}
			}
			if bytes.Contains(chunk, needleBytes) {
				return true, nil
			}
			tail = updateSearchTail(tail, chunk, overlap)
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			return false, nil
		}
		return false, readErr
	}
}

func textSearchBufferSize(f *os.File) int {
	size := textSearchChunkSize
	info, err := f.Stat()
	if err == nil && info.Size() > 0 && info.Size() < int64(size) {
		size = int(info.Size())
	}
	return size
}

func updateSearchTail(tail, chunk []byte, count int) []byte {
	if count <= 0 {
		return nil
	}
	if len(chunk) >= count {
		tail = tail[:count]
		copy(tail, chunk[len(chunk)-count:])
		return tail
	}
	needed := count - len(chunk)
	if len(tail) > needed {
		tail = tail[len(tail)-needed:]
	}
	next := make([]byte, 0, count)
	next = append(next, tail...)
	next = append(next, chunk...)
	return next
}

func isTextSearchKind(kind string) bool {
	switch kind {
	case "anim", "asmdef", "asmref", "asset", "controller", "cs", "json", "mat",
		"md", "playable", "prefab", "scene", "shader", "shadergraph",
		"shadersubgraph", "shadervariants", "txt", "uss", "uxml", "xml", "yml":
		return true
	default:
		return false
	}
}

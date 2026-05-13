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
	name         string
	component    string
	scriptPath   string
	source       string
	guid         string
	ref          string
	types        string
	compact      bool
	warningsMode string
	limit        int
	objectLimit  int
	rootPath     string
	scriptScoped bool
	refDetail    bool
	guidIndex    unityasset.GUIDIndex
	sourceIndex  unityasset.GUIDIndex
	sourceGUIDs  map[string]bool
}

type searchMatch struct {
	File        unityasset.FileEntry
	Objects     []objectMatch
	Refs        []refMatch
	SourcePaths []string
	RawGUID     bool
	FileNameHit bool
}

type refMatch struct {
	Object    string
	Component string
	Field     string
	Value     string
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

var currentSearchLineWidth = 1200

func searchCmd(args []string) error {
	opts := searchOptions{objectLimit: 12}
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	addCommonFlags(fs, &opts.commonOptions)
	fs.StringVar(&opts.name, "name", "", "file or GameObject name")
	fs.StringVar(&opts.component, "component", "", "component or script name")
	fs.StringVar(&opts.scriptPath, "script-path", "", "match MonoBehaviour scripts under asset path")
	fs.StringVar(&opts.source, "source", "", "match prefab source path/name")
	fs.StringVar(&opts.guid, "guid", "", "raw Unity GUID")
	fs.StringVar(&opts.ref, "ref", "", "raw Unity GUID alias")
	fs.StringVar(&opts.types, "type", "", "comma-separated asset kinds")
	fs.BoolVar(&opts.compact, "compact", false, "compact output")
	fs.StringVar(&opts.warningsMode, "warnings", "summary", "warning output: summary or detail")
	fs.IntVar(&opts.limit, "limit", opts.limit, "max result files")
	fs.IntVar(&opts.objectLimit, "object-limit", opts.objectLimit, "max objects shown per result file")
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
	if opts.name == "" && opts.component == "" && opts.scriptPath == "" && opts.source == "" && opts.guid == "" {
		return fmt.Errorf("search requires --name, --component, --script-path, --source, --guid, or --ref")
	}
	profile := newCommandProfile(opts.profile)

	target := "Assets"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	project, err := unityasset.OpenProject(opts.project)
	if err != nil {
		return err
	}
	profile.mark("open_project")
	kinds := unityasset.ParseKindSet(opts.types)
	kinds = defaultSearchKinds(kinds, opts)
	result, err := unityasset.Scan(project, target, unityasset.ScanOptions{
		Kinds:   kinds,
		Workers: opts.workers,
	})
	if err != nil {
		return err
	}
	profile.mark("scan")

	scripts := unityasset.ScriptIndex{}
	if opts.scriptPath != "" {
		scripts, err = unityasset.BuildScriptIndexForPath(project, opts.scriptPath)
		if err != nil {
			return err
		}
		opts.scriptScoped = true
	} else if opts.component != "" && !unityasset.MatchesNativeClassName(opts.component) {
		scripts, err = unityasset.BuildScriptIndexForQuery(project, opts.component)
		if err != nil {
			return err
		}
		opts.scriptScoped = true
	}
	profile.mark("build_script_index")
	sourceIndexFromFiles := false
	if opts.source != "" {
		opts.sourceIndex = buildSourceIndexFromFiles(result.Files, opts.source)
		sourceIndexFromFiles = len(opts.sourceIndex) > 0
		if !sourceIndexFromFiles {
			opts.sourceIndex, err = unityasset.BuildGUIDIndexForPathQuery(project, opts.source)
			if err != nil {
				return err
			}
		}
		opts.sourceGUIDs = guidSetFromIndex(opts.sourceIndex)
	}
	profile.mark("build_source_index")

	_, opts.rootPath, _ = project.Resolve(target)
	matches, warnings := runSearch(project, result.Files, scripts, opts)
	profile.mark("search_files")
	if opts.source != "" && sourceIndexFromFiles && len(matches) == 0 {
		opts.sourceIndex, err = unityasset.BuildGUIDIndexForPathQuery(project, opts.source)
		if err != nil {
			return err
		}
		opts.sourceGUIDs = guidSetFromIndex(opts.sourceIndex)
		profile.mark("build_source_fallback")
		matches, warnings = runSearch(project, result.Files, scripts, opts)
		profile.mark("search_files_fallback")
	}
	printSearch(matches, result.KindCount, opts, warnings)
	profile.mark("print")
	profile.print()
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

	workers := opts.workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan int)
	results := make(chan searchFileResult, workers)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			guidSearcher := makeTextSearcher(opts.guid)
			sourceSearcher := makeMultiTextSearcher(opts.sourceGUIDs)
			for index := range jobs {
				result := searchOneFile(project, index, files[index], scripts, opts, &guidSearcher, sourceSearcher)
				if result.Matched || len(result.Warnings) > 0 {
					results <- result
				}
			}
		}()
	}

	go func() {
		for i := range files {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	collected := make([]searchFileResult, 0)
	for result := range results {
		collected = append(collected, result)
	}
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].Index < collected[j].Index
	})

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
	guidSearcher := makeTextSearcher(opts.guid)
	sourceSearcher := makeMultiTextSearcher(opts.sourceGUIDs)
	for i, file := range files {
		result := searchOneFile(project, i, file, scripts, opts, &guidSearcher, sourceSearcher)
		warnings = append(warnings, result.Warnings...)
		if result.Matched {
			matches = append(matches, result.Match)
		}
	}
	return matches, warnings
}

func searchOneFile(project unityasset.Project, index int, file unityasset.FileEntry, scripts unityasset.ScriptIndex, opts searchOptions, guidSearcher *textSearcher, sourceSearcher *multiTextSearcher) searchFileResult {
	result := searchFileResult{Index: index, Match: searchMatch{File: file}}
	if file.IsMeta {
		return result
	}

	if opts.guid != "" {
		ok, err := guidSearcher.contains(file)
		if err != nil {
			result.Warnings = append(result.Warnings, searchWarning{Path: file.AssetPath, Err: err})
		}
		result.Match.RawGUID = ok
	}
	if opts.name != "" && opts.component == "" && containsFold(file.Name, opts.name) {
		result.Match.FileNameHit = true
		result.Matched = true
		return result
	}

	sourceCandidate := true
	if opts.source != "" {
		var err error
		sourceCandidate, err = sourceSearcher.contains(file)
		if err != nil {
			result.Warnings = append(result.Warnings, searchWarning{Path: file.AssetPath, Err: err})
		}
	}
	needsStructured := opts.name != "" || opts.component != "" || opts.scriptPath != "" || (opts.source != "" && sourceCandidate) || opts.refDetail
	if needsStructured && unityasset.KnownUnityYAMLKind(file.Kind) {
		var asset *unityasset.Asset
		var err error
		if opts.refDetail {
			if !result.Match.RawGUID {
				return result
			}
			asset, err = unityasset.ReadAsset(file, scripts)
			if err == nil && len(opts.guidIndex) > 0 {
				asset.GUIDIndex = opts.guidIndex
			}
		} else {
			asset, err = unityasset.ReadAssetSummary(file, scripts)
		}
		if err == nil {
			if opts.source != "" {
				sourcePaths := sourcePaths(asset.SourcePrefabGUIDs(), opts.sourceIndex)
				result.Match.SourcePaths = sourcePaths
				if sourceMatches(sourcePaths, opts.source) {
					result.Matched = true
				}
			}
			if opts.name != "" || opts.component != "" || opts.scriptPath != "" {
				result.Match.Objects = objectMatches(asset, opts)
			}
			if opts.refDetail && result.Match.RawGUID {
				result.Match.Refs, err = referenceMatchesWithResolvedComponents(project, asset, opts.guid)
				if err != nil {
					result.Warnings = append(result.Warnings, searchWarning{Path: file.AssetPath, Err: err})
				}
			}
		} else {
			result.Warnings = append(result.Warnings, searchWarning{Path: file.AssetPath, Err: err})
		}
	}

	result.Matched = result.Matched || result.Match.RawGUID || result.Match.FileNameHit || len(result.Match.Objects) > 0
	return result
}

func sourceMatches(paths []string, query string) bool {
	for _, path := range paths {
		if containsFold(path, query) {
			return true
		}
	}
	return false
}

func guidSetFromIndex(index unityasset.GUIDIndex) map[string]bool {
	if len(index) == 0 {
		return nil
	}
	out := make(map[string]bool, len(index))
	for guid := range index {
		out[strings.ToLower(guid)] = true
	}
	return out
}

func buildSourceIndexFromFiles(files []unityasset.FileEntry, query string) unityasset.GUIDIndex {
	index := unityasset.GUIDIndex{}
	for _, file := range files {
		if file.IsMeta || !containsFold(file.AssetPath, query) {
			continue
		}
		guid := unityasset.ReadMetaGUID(file.Abs + ".meta")
		if guid != "" {
			index[strings.ToLower(guid)] = file.AssetPath
		}
	}
	return index
}

func objectMatches(asset *unityasset.Asset, opts searchOptions) []objectMatch {
	var out []objectMatch
	for _, node := range asset.FlattenNodes() {
		nameOK := opts.name == "" || containsFold(node.GameObject.Name, opts.name)
		components := asset.ComponentsFor(node.GameObject.ID)
		componentNames := make([]string, 0, len(components))
		componentOK := opts.component == "" && opts.scriptPath == ""
		for _, component := range components {
			componentNames = append(componentNames, component.Name)
			if componentMatches(component, opts) {
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
			componentOK := (opts.component == "" && opts.scriptPath == "") || componentObjectMatches(obj, name, scriptPath, opts)
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

func componentMatches(component unityasset.Component, opts searchOptions) bool {
	if opts.scriptPath != "" {
		return pathUnder(component.ScriptPath, opts.scriptPath)
	}
	if opts.component == "" {
		return true
	}
	if opts.scriptScoped && component.Object != nil && component.Object.Type == "MonoBehaviour" && component.Object.ScriptGUID != "" && component.ScriptPath == "" {
		return false
	}
	return containsFold(component.Name, opts.component)
}

func componentObjectMatches(obj *unityasset.Object, name, scriptPath string, opts searchOptions) bool {
	if opts.scriptPath != "" {
		return pathUnder(scriptPath, opts.scriptPath)
	}
	if opts.component == "" {
		return true
	}
	if opts.scriptScoped && obj != nil && obj.Type == "MonoBehaviour" && obj.ScriptGUID != "" && scriptPath == "" {
		return false
	}
	return containsFold(name, opts.component) || containsFold(scriptPath, opts.component)
}

func filepathSlash(path string) string {
	return strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
}

func pathUnder(path, root string) bool {
	path = filepathSlash(path)
	root = filepathSlash(root)
	return path == root || strings.HasPrefix(path, root+"/")
}

func referenceMatchesWithResolvedComponents(project unityasset.Project, asset *unityasset.Asset, guid string) ([]refMatch, error) {
	refs := asset.FieldReferences(guid)
	wanted := map[string]bool{}
	for _, ref := range refs {
		if ref.Object != nil && ref.Object.ScriptGUID != "" {
			wanted[ref.Object.ScriptGUID] = true
		}
	}
	if len(wanted) > 0 {
		scripts, err := unityasset.BuildScriptIndexForGUIDs(project, wanted)
		if err != nil {
			return nil, err
		}
		asset.ScriptIndex = scripts
	}
	out := make([]refMatch, 0, len(refs))
	for _, ref := range refs {
		component, objectPath := componentAndObjectPath(asset, ref.Object)
		out = append(out, refMatch{Object: objectPath, Component: component, Field: ref.FieldName, Value: ref.Value})
	}
	return out, nil
}

func componentAndObjectPath(asset *unityasset.Asset, obj *unityasset.Object) (string, string) {
	name, _ := asset.ComponentName(obj)
	if obj == nil {
		return name, ""
	}
	if obj.GameObjectID != "" {
		return name, asset.ObjectPath(obj.GameObjectID)
	}
	if obj.Name != "" {
		return name, obj.Name
	}
	return name, fmt.Sprintf("object %s", obj.ID)
}

func printSearch(matches []searchMatch, kindCounts map[string]int, opts searchOptions, warnings []searchWarning) {
	currentSearchLineWidth = opts.lineWidth
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
		printSearchWarnings(warnings, opts.warningsMode)
		return
	}

	printSearchDetailed(matches[:opts.limit], opts)
	if len(matches) > opts.limit {
		fmt.Printf("\nmore: %d hidden by --limit\n", len(matches)-opts.limit)
	}
	printSearchWarnings(warnings, opts.warningsMode)
}

func printSearchDetailed(matches []searchMatch, opts searchOptions) {
	for _, group := range groupSearchMatches(matches) {
		fmt.Printf("[%s] %s\n", group.Kind, compactGroupDir(group.Dir, opts.rootPath))
		for _, match := range group.Matches {
			printSearchMatch(match, opts)
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

func printSearchMatch(match searchMatch, opts searchOptions) {
	fmt.Printf("  %s\n", match.File.Name)
	if match.FileNameHit {
		fmt.Println("    file-name")
	}
	if match.RawGUID {
		fmt.Println("    guid-reference")
	}
	for _, source := range match.SourcePaths {
		printfLineLimited(currentSearchLineWidth, "    source: %s", source)
	}
	for _, ref := range match.Refs {
		if ref.Object != "" {
			printfLineLimited(currentSearchLineWidth, "    object: %s", ref.Object)
		}
		if ref.Component != "" {
			printfLineLimited(currentSearchLineWidth, "    component: %s", ref.Component)
		}
		printfLineLimited(currentSearchLineWidth, "    field: %s", ref.Field)
		if ref.Value != "" {
			printfLineLimited(currentSearchLineWidth, "    value: %s", ref.Value)
		}
	}
	objectLimit := len(match.Objects)
	if objectLimit > opts.objectLimit {
		objectLimit = opts.objectLimit
	}
	if objectLimit < 0 {
		objectLimit = 0
	}
	for _, obj := range match.Objects[:objectLimit] {
		printfLineLimited(currentSearchLineWidth, "    object: %s", obj.Path)
		if len(obj.Components) > 0 {
			printfLineLimited(currentSearchLineWidth, "    components: %s", strings.Join(obj.Components, ", "))
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
			printfLineLimited(currentSearchLineWidth, "  %s :: %s", compactGroupDir(parts[1], rootPath), line)
		}
	}
}

func printSearchWarnings(warnings []searchWarning, mode string) {
	if len(warnings) == 0 {
		return
	}
	if mode != "detail" {
		counts := map[string]int{}
		for _, warning := range warnings {
			counts[warning.Err.Error()]++
		}
		fmt.Printf("\nwarnings: %d skipped\n", len(warnings))
		keys := make([]string, 0, len(counts))
		for key := range counts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Printf("  %s: %d\n", key, counts[key])
		}
		fmt.Println("hint: use --warnings detail")
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
	searcher := makeTextSearcher(needle)
	return searcher.contains(file)
}

type textSearcher struct {
	needle []byte
	buf    []byte
	tail   []byte
	bridge []byte
}

type multiTextSearcher struct {
	searchers []textSearcher
}

func makeTextSearcher(needle string) textSearcher {
	return textSearcher{needle: []byte(needle)}
}

func makeMultiTextSearcher(needles map[string]bool) *multiTextSearcher {
	if len(needles) == 0 {
		return &multiTextSearcher{}
	}
	searchers := make([]textSearcher, 0, len(needles))
	for needle := range needles {
		searchers = append(searchers, makeTextSearcher(needle))
	}
	return &multiTextSearcher{searchers: searchers}
}

func (s *multiTextSearcher) contains(file unityasset.FileEntry) (bool, error) {
	if s == nil || len(s.searchers) == 0 {
		return false, nil
	}
	for i := range s.searchers {
		ok, err := s.searchers[i].contains(file)
		if ok || err != nil {
			return ok, err
		}
	}
	return false, nil
}

func (s *textSearcher) contains(file unityasset.FileEntry) (bool, error) {
	if !isTextSearchKind(file.Kind) {
		return false, nil
	}
	f, err := os.Open(file.Abs)
	if err != nil {
		return false, err
	}
	defer f.Close()

	if len(s.needle) == 0 {
		return true, nil
	}

	overlap := len(s.needle) - 1
	buf := s.buffer(textSearchBufferSize(f))
	tail := s.tailBuffer(overlap)
	bridge := s.bridgeBuffer(overlap * 2)
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
				if bytes.Contains(bridge, s.needle) {
					return true, nil
				}
			}
			if bytes.Contains(chunk, s.needle) {
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

func (s *textSearcher) buffer(size int) []byte {
	if cap(s.buf) < size {
		s.buf = make([]byte, size)
	}
	return s.buf[:size]
}

func (s *textSearcher) tailBuffer(size int) []byte {
	if cap(s.tail) < size {
		s.tail = make([]byte, 0, size)
	}
	return s.tail[:0]
}

func (s *textSearcher) bridgeBuffer(size int) []byte {
	if cap(s.bridge) < size {
		s.bridge = make([]byte, 0, size)
	}
	return s.bridge[:0]
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
		copy(tail, tail[len(tail)-needed:])
		tail = tail[:needed]
	}
	return append(tail, chunk...)
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

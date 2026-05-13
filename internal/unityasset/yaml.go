package unityasset

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var nativeClassNames = map[int]string{
	1:        "GameObject",
	4:        "Transform",
	20:       "Camera",
	23:       "MeshRenderer",
	33:       "MeshFilter",
	64:       "MeshCollider",
	65:       "BoxCollider",
	81:       "AudioListener",
	82:       "AudioSource",
	95:       "Animator",
	108:      "Light",
	114:      "MonoBehaviour",
	115:      "MonoScript",
	120:      "LineRenderer",
	137:      "SkinnedMeshRenderer",
	156:      "Terrain",
	198:      "ParticleSystem",
	212:      "SpriteRenderer",
	222:      "CanvasRenderer",
	223:      "Canvas",
	224:      "RectTransform",
	225:      "CanvasGroup",
	329:      "VideoPlayer",
	73398921: "VFXRenderer",
}

var (
	nameLinePrefix         = []byte("  m_Name:")
	gameObjectLinePrefix   = []byte("  m_GameObject:")
	fatherLinePrefix       = []byte("  m_Father:")
	sourceObjectLinePrefix = []byte("  m_CorrespondingSourceObject:")
	scriptLinePrefix       = []byte("  m_Script:")
	componentLineMarker    = []byte("- component:")
)

type Asset struct {
	Path        string
	Kind        string
	GUID        string
	Objects     []*Object
	ByID        map[string]*Object
	ScriptIndex ScriptIndex
	GUIDIndex   GUIDIndex
	SourcePaths []string
}

type Object struct {
	ID                string
	ClassID           int
	Type              string
	Lines             []string
	Order             int
	Name              string
	ComponentIDs      []string
	GameObjectID      string
	FatherTransformID string
	SourceObjectID    string
	SourceGUID        string
	ScriptGUID        string
}

type Node struct {
	GameObject *Object
	Children   []*Node
	Path       string
	Depth      int
}

type Component struct {
	Object     *Object
	Name       string
	ScriptPath string
}

type Field struct {
	Name  string
	Value string
}

type FieldReference struct {
	Object    *Object
	FieldName string
	Value     string
}

type PrefabOverride struct {
	Kind         string
	Target       string
	PropertyPath string
	Value        string
	AddedObject  string
}

type ScriptIndex map[string]string

type GUIDIndex map[string]string

type ParseOptions struct {
	KeepLines bool
}

func ReadAsset(entry FileEntry, scripts ScriptIndex) (*Asset, error) {
	return readAssetWithOptions(entry, scripts, ParseOptions{KeepLines: true}, true)
}

func ReadAssetSummary(entry FileEntry, scripts ScriptIndex) (*Asset, error) {
	return readAssetWithOptions(entry, scripts, ParseOptions{}, false)
}

func readAssetWithOptions(entry FileEntry, scripts ScriptIndex, opts ParseOptions, readMeta bool) (*Asset, error) {
	data, err := os.ReadFile(entry.Abs)
	if err != nil {
		return nil, err
	}
	asset, err := ParseAssetWithOptions(data, opts)
	if err != nil {
		return nil, err
	}
	asset.Path = entry.AssetPath
	asset.Kind = entry.Kind
	if readMeta {
		asset.GUID = ReadMetaGUID(entry.Abs + ".meta")
	}
	asset.ScriptIndex = scripts
	return asset, nil
}

func ParseAsset(data []byte) (*Asset, error) {
	return ParseAssetWithOptions(data, ParseOptions{KeepLines: true})
}

func ParseAssetSummary(data []byte) (*Asset, error) {
	return ParseAssetWithOptions(data, ParseOptions{})
}

func ParseAssetWithOptions(data []byte, opts ParseOptions) (*Asset, error) {
	asset := &Asset{ByID: map[string]*Object{}}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024*16)

	var current *Object
	for scanner.Scan() {
		line := scanner.Bytes()
		if classID, id, ok := parseHeaderLine(line); ok {
			if current != nil {
				asset.finishObject(current)
			}
			current = &Object{
				ID:      id,
				ClassID: classID,
				Order:   len(asset.Objects),
			}
			continue
		}
		if current == nil {
			continue
		}
		if opts.KeepLines {
			current.Lines = append(current.Lines, string(line))
			scanObjectTypeLine(current, line)
			continue
		}
		scanObjectLine(current, line)
		if current.Type == "PrefabInstance" {
			current.Lines = append(current.Lines, string(line))
		}
	}
	if current != nil {
		asset.finishObject(current)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(asset.Objects) == 0 {
		return nil, fmt.Errorf("no Unity YAML objects found")
	}
	asset.applyPrefabNameOverrides()
	return asset, nil
}

func (a *Asset) finishObject(o *Object) {
	if len(o.Lines) > 0 {
		o.Name = readScalar(o.Lines, "m_Name")
		switch o.Type {
		case "GameObject":
			o.ComponentIDs = readComponentIDs(o.Lines)
		case "Transform", "RectTransform":
			o.GameObjectID = readFileIDField(o.Lines, "m_GameObject")
			o.FatherTransformID = readFileIDField(o.Lines, "m_Father")
		default:
			o.GameObjectID = readFileIDField(o.Lines, "m_GameObject")
		}
		o.SourceObjectID = readFileIDField(o.Lines, "m_CorrespondingSourceObject")
		o.SourceGUID = readGUIDField(o.Lines, "m_CorrespondingSourceObject")
		if o.Type == "MonoBehaviour" {
			o.ScriptGUID = readGUIDField(o.Lines, "m_Script")
		}
	}
	a.Objects = append(a.Objects, o)
	a.ByID[o.ID] = o
}

func (a *Asset) GameObjects() []*Object {
	out := make([]*Object, 0)
	for _, obj := range a.Objects {
		if obj.Type == "GameObject" {
			out = append(out, obj)
		}
	}
	return out
}

func (a *Asset) ScriptGUIDs() map[string]bool {
	guids := map[string]bool{}
	for _, obj := range a.Objects {
		if obj.ScriptGUID != "" {
			guids[obj.ScriptGUID] = true
		}
	}
	return guids
}

func AddFieldGUIDs(guids map[string]bool, obj *Object) {
	if obj == nil {
		return
	}
	for _, line := range obj.Lines {
		addLineGUIDs(guids, line)
	}
}

func AddVisibleFieldGUIDs(guids map[string]bool, obj *Object, limit int) {
	if obj == nil {
		return
	}
	visible := 0
	for i := 0; i < len(obj.Lines); i++ {
		line := obj.Lines[i]
		if !isTopLevelFieldLine(line) {
			continue
		}
		trim := strings.TrimSpace(line)
		parts := strings.SplitN(trim, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if skipField(key) {
			continue
		}
		if limit > 0 && visible >= limit {
			return
		}
		visible++
		addLineGUIDs(guids, line)
		if strings.TrimSpace(parts[1]) != "" {
			continue
		}
		for j := i + 1; j < len(obj.Lines) && !isTopLevelFieldLine(obj.Lines[j]); j++ {
			addLineGUIDs(guids, obj.Lines[j])
		}
	}
}

func addLineGUIDs(guids map[string]bool, line string) {
	if !strings.Contains(line, "guid:") || strings.Contains(line, "m_Script") {
		return
	}
	for _, guid := range findGUIDs(line) {
		guids[guid] = true
	}
}

func (a *Asset) ComponentsFor(goID string) []Component {
	goObj := a.ByID[goID]
	if goObj == nil {
		return nil
	}
	out := make([]Component, 0, len(goObj.ComponentIDs))
	seen := map[string]bool{}
	for _, compID := range goObj.ComponentIDs {
		compObj := a.ByID[compID]
		if compObj == nil {
			continue
		}
		seen[compObj.ID] = true
		name, scriptPath := a.ComponentName(compObj)
		out = append(out, Component{Object: compObj, Name: name, ScriptPath: scriptPath})
	}
	for _, compObj := range a.Objects {
		if compObj.GameObjectID != goID || seen[compObj.ID] || compObj.Type == "GameObject" {
			continue
		}
		seen[compObj.ID] = true
		name, scriptPath := a.ComponentName(compObj)
		out = append(out, Component{Object: compObj, Name: name, ScriptPath: scriptPath})
	}
	return out
}

func (a *Asset) ComponentName(obj *Object) (string, string) {
	if obj == nil {
		return "", ""
	}
	if obj.Type == "MonoBehaviour" {
		if path := a.ScriptIndex[obj.ScriptGUID]; path != "" {
			return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), path
		}
		if obj.ScriptGUID != "" {
			return "MonoBehaviour(" + shortGUID(obj.ScriptGUID) + ")", ""
		}
	}
	if native := NativeClassName(obj.ClassID); native != "" {
		return native, ""
	}
	if obj.Type != "" {
		return obj.Type, ""
	}
	return fmt.Sprintf("ClassID:%d", obj.ClassID), ""
}

func (a *Asset) Hierarchy() []*Node {
	transformToGO := map[string]string{}
	goToTransform := map[string]string{}
	parentTransform := map[string]string{}

	for _, obj := range a.Objects {
		if obj.Type != "Transform" && obj.Type != "RectTransform" {
			continue
		}
		transformToGO[obj.ID] = obj.GameObjectID
		goToTransform[obj.GameObjectID] = obj.ID
		parentTransform[obj.ID] = obj.FatherTransformID
	}

	gameObjects := a.GameObjects()
	nodes := make(map[string]*Node, len(gameObjects))
	for _, goObj := range gameObjects {
		nodes[goObj.ID] = &Node{GameObject: goObj}
	}

	hasParent := map[string]bool{}
	for goID, transformID := range goToTransform {
		node := nodes[goID]
		if node == nil {
			continue
		}
		parentGO := transformToGO[parentTransform[transformID]]
		parent := nodes[parentGO]
		if parent == nil || parent == node {
			continue
		}
		parent.Children = append(parent.Children, node)
		hasParent[goID] = true
	}

	roots := make([]*Node, 0)
	for _, goObj := range gameObjects {
		node := nodes[goObj.ID]
		if node == nil || hasParent[goObj.ID] {
			continue
		}
		roots = append(roots, node)
	}

	sortNodes(roots)
	for _, root := range roots {
		assignNodePath(root, "", 0)
	}
	return roots
}

func (a *Asset) FlattenNodes() []*Node {
	var out []*Node
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, node := range nodes {
			out = append(out, node)
			walk(node.Children)
		}
	}
	walk(a.Hierarchy())
	return out
}

func (a *Asset) Fields(obj *Object, limit int) []Field {
	fields, _ := a.FieldsWithHidden(obj, limit)
	return fields
}

func (a *Asset) FieldsWithHidden(obj *Object, limit int) ([]Field, int) {
	fields := make([]Field, 0)
	hidden := 0
	for i := 0; i < len(obj.Lines); i++ {
		line := obj.Lines[i]
		if !isTopLevelFieldLine(line) {
			continue
		}
		trim := strings.TrimSpace(line)
		parts := strings.SplitN(trim, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if skipField(key) {
			continue
		}
		if limit > 0 && len(fields) >= limit {
			hidden++
			continue
		}
		value := strings.TrimSpace(parts[1])
		if value == "" {
			value = summarizeNested(obj.Lines, i+1)
		}
		value = a.ResolveReferences(value)
		fields = append(fields, Field{Name: DisplayFieldName(key), Value: value})
	}
	return fields, hidden
}

func DisplayFieldName(name string) string {
	if strings.HasPrefix(name, "<") && strings.HasSuffix(name, ">k__BackingField") {
		return strings.TrimSuffix(strings.TrimPrefix(name, "<"), ">k__BackingField")
	}
	return name
}

func (a *Asset) ResolveReferences(value string) string {
	if len(a.GUIDIndex) == 0 {
		return value
	}
	guids := findGUIDs(value)
	if len(guids) == 0 {
		return value
	}

	paths := make([]string, 0, len(guids))
	seen := map[string]bool{}
	for _, guid := range guids {
		path := a.GUIDIndex[guid]
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return value
	}
	return value + " -> " + strings.Join(paths, ", ")
}

func summarizeNested(lines []string, start int) string {
	const maxParts = 4
	parts := make([]string, 0, maxParts)
	parent := ""
	sequenceIndex := 0
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "  -") {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "- ") {
			trim = strings.TrimPrefix(trim, "- ")
			if parent != "" {
				trim = fmt.Sprintf("%s[%d]: %s", parent, sequenceIndex, trim)
				sequenceIndex++
			}
		} else if strings.HasSuffix(trim, ":") {
			parent = strings.TrimSuffix(trim, ":")
			sequenceIndex = 0
			continue
		}
		trim = strings.Join(strings.Fields(trim), " ")
		parts = append(parts, trim)
		if len(parts) >= maxParts {
			break
		}
	}
	if len(parts) == 0 {
		return "<object>"
	}
	return strings.Join(parts, " | ")
}

func (a *Asset) SourcePrefabGUIDs() []string {
	seen := map[string]bool{}
	var out []string
	for _, obj := range a.Objects {
		if obj.Type != "PrefabInstance" {
			continue
		}
		guid := readGUIDField(obj.Lines, "m_SourcePrefab")
		if guid == "" || seen[guid] {
			continue
		}
		seen[guid] = true
		out = append(out, guid)
	}
	return out
}

func (a *Asset) PrefabOverrides() []PrefabOverride {
	var out []PrefabOverride
	for _, obj := range a.Objects {
		if obj.Type != "PrefabInstance" {
			continue
		}
		out = append(out, parsePrefabOverrides(obj.Lines)...)
	}
	return out
}

func parsePrefabOverrides(lines []string) []PrefabOverride {
	var out []PrefabOverride
	section := ""
	for i := 0; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		switch trim {
		case "m_Modifications:":
			section = "modifications"
			continue
		case "m_RemovedComponents:":
			section = "removed-components"
			continue
		case "m_RemovedGameObjects:":
			section = "removed-gameobjects"
			continue
		case "m_AddedComponents:":
			section = "added-components"
			continue
		case "m_AddedGameObjects:":
			section = "added-gameobjects"
			continue
		}
		if !strings.HasPrefix(trim, "- ") {
			continue
		}
		switch section {
		case "modifications":
			mod, next := parsePrefabPropertyOverride(lines, i)
			if mod.Target != "" {
				out = append(out, mod)
			}
			i = next
		case "removed-components", "removed-gameobjects":
			target := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
			if target != "" && target != "[]" {
				out = append(out, PrefabOverride{Kind: section, Target: target})
			}
		case "added-components":
			mod, next := parsePrefabAddedComponentOverride(lines, i)
			if mod.Target != "" || mod.AddedObject != "" {
				out = append(out, mod)
			}
			i = next
		}
	}
	return out
}

func parsePrefabPropertyOverride(lines []string, start int) (PrefabOverride, int) {
	out := PrefabOverride{Kind: "property"}
	for i := start; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if i > start && isPrefabOverrideSection(trim) {
			return out, i - 1
		}
		if i > start && strings.HasPrefix(trim, "- ") {
			return out, i - 1
		}
		switch {
		case strings.HasPrefix(trim, "- target:"):
			out.Target = strings.TrimSpace(strings.TrimPrefix(trim, "- target:"))
		case strings.HasPrefix(trim, "propertyPath:"):
			out.PropertyPath = DisplayFieldName(cleanScalar(strings.TrimSpace(strings.TrimPrefix(trim, "propertyPath:"))))
		case strings.HasPrefix(trim, "value:"):
			out.Value = cleanScalar(strings.TrimSpace(strings.TrimPrefix(trim, "value:")))
		case strings.HasPrefix(trim, "objectReference:") && out.Value == "":
			out.Value = strings.TrimSpace(strings.TrimPrefix(trim, "objectReference:"))
		}
	}
	return out, len(lines) - 1
}

func parsePrefabAddedComponentOverride(lines []string, start int) (PrefabOverride, int) {
	out := PrefabOverride{Kind: "added-component"}
	for i := start; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if i > start && isPrefabOverrideSection(trim) {
			return out, i - 1
		}
		if i > start && strings.HasPrefix(trim, "- ") {
			return out, i - 1
		}
		switch {
		case strings.HasPrefix(trim, "- targetCorrespondingSourceObject:"):
			out.Target = strings.TrimSpace(strings.TrimPrefix(trim, "- targetCorrespondingSourceObject:"))
		case strings.HasPrefix(trim, "addedObject:"):
			out.AddedObject = strings.TrimSpace(strings.TrimPrefix(trim, "addedObject:"))
		}
	}
	return out, len(lines) - 1
}

func isPrefabOverrideSection(trim string) bool {
	switch trim {
	case "m_Modifications:", "m_RemovedComponents:", "m_RemovedGameObjects:", "m_AddedComponents:", "m_AddedGameObjects:":
		return true
	default:
		return false
	}
}

func (a *Asset) FieldReferences(targetGUID string) []FieldReference {
	var refs []FieldReference
	targetGUID = strings.ToLower(targetGUID)
	for _, obj := range a.Objects {
		for i := 0; i < len(obj.Lines); i++ {
			line := obj.Lines[i]
			if !isTopLevelFieldLine(line) {
				continue
			}
			trim := strings.TrimSpace(line)
			parts := strings.SplitN(trim, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			if skipField(key) {
				continue
			}
			value := strings.TrimSpace(parts[1])
			if value == "" {
				nested, ok := summarizeNestedReferenceMatch(obj.Lines, i+1, targetGUID)
				if !ok {
					continue
				}
				value = nested
			} else {
				if !containsGUID(value, targetGUID) {
					continue
				}
				value = referenceExcerpt(value, targetGUID)
			}
			refs = append(refs, FieldReference{Object: obj, FieldName: DisplayFieldName(key), Value: a.ResolveReferences(value)})
		}
		refs = append(refs, a.prefabModificationReferences(obj, targetGUID)...)
	}
	return refs
}

func summarizeNestedReferenceMatch(lines []string, start int, targetGUID string) (string, bool) {
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "  -") {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" || !containsGUID(trim, targetGUID) {
			continue
		}
		return referenceExcerpt(strings.Join(strings.Fields(trim), " "), targetGUID), true
	}
	return "", false
}

func referenceExcerpt(value string, targetGUID string) string {
	const radius = 160
	if len(value) <= radius*2 {
		return value
	}
	lowerValue := strings.ToLower(value)
	index := strings.Index(lowerValue, strings.ToLower(targetGUID))
	if index < 0 {
		return value[:radius*2] + "..."
	}
	start := index - radius
	if start < 0 {
		start = 0
	}
	end := index + len(targetGUID) + radius
	if end > len(value) {
		end = len(value)
	}
	excerpt := value[start:end]
	if start > 0 {
		excerpt = "..." + excerpt
	}
	if end < len(value) {
		excerpt += "..."
	}
	return excerpt
}

func (a *Asset) prefabModificationReferences(obj *Object, targetGUID string) []FieldReference {
	if obj == nil || obj.Type != "PrefabInstance" {
		return nil
	}
	var refs []FieldReference
	propertyPath := ""
	for _, line := range obj.Lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "propertyPath:") {
			propertyPath = DisplayFieldName(cleanScalar(strings.TrimSpace(strings.TrimPrefix(trim, "propertyPath:"))))
			continue
		}
		if propertyPath == "" || !strings.Contains(trim, "guid:") || !containsGUID(trim, targetGUID) {
			continue
		}
		refs = append(refs, FieldReference{Object: obj, FieldName: propertyPath, Value: a.ResolveReferences(trim)})
		propertyPath = ""
	}
	return refs
}

func containsGUID(value, guid string) bool {
	for _, found := range findGUIDs(value) {
		if strings.EqualFold(found, guid) {
			return true
		}
	}
	return false
}

func BuildScriptIndexForPath(p Project, scriptPath string) (ScriptIndex, error) {
	index := ScriptIndex{}
	scriptPath = strings.Trim(filepath.ToSlash(scriptPath), "/")
	if scriptPath == "" {
		return index, nil
	}
	err := filepath.WalkDir(p.Assets, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != p.Assets {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".cs.meta") {
			return nil
		}
		assetPath := p.AssetPath(strings.TrimSuffix(path, ".meta"))
		if !strings.HasPrefix(filepath.ToSlash(assetPath), scriptPath) {
			return nil
		}
		guid := ReadMetaGUID(path)
		if guid != "" {
			index[strings.ToLower(guid)] = assetPath
		}
		return nil
	})
	return index, err
}

func isTopLevelFieldLine(line string) bool {
	return strings.HasPrefix(line, "  ") &&
		!strings.HasPrefix(line, "    ") &&
		!strings.HasPrefix(line, "  -")
}

func (a *Asset) ObjectPath(goID string) string {
	for _, node := range a.FlattenNodes() {
		if node.GameObject.ID == goID {
			return node.Path
		}
	}
	if obj := a.ByID[goID]; obj != nil {
		return obj.Name
	}
	return ""
}

func BuildScriptIndex(p Project) (ScriptIndex, error) {
	return BuildScriptIndexForGUIDs(p, nil)
}

func BuildScriptIndexForQuery(p Project, query string) (ScriptIndex, error) {
	index := ScriptIndex{}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return index, nil
	}
	err := filepath.WalkDir(p.Assets, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != p.Assets {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".cs.meta") {
			return nil
		}
		scriptPath := strings.TrimSuffix(path, ".meta")
		assetPath := p.AssetPath(scriptPath)
		scriptName := strings.TrimSuffix(filepath.Base(scriptPath), filepath.Ext(scriptPath))
		if !containsLower(scriptName, query) {
			return nil
		}
		guid := ReadMetaGUID(path)
		if guid == "" {
			return nil
		}
		index[strings.ToLower(guid)] = assetPath
		return nil
	})
	return index, err
}

func BuildScriptIndexForGUIDs(p Project, wanted map[string]bool) (ScriptIndex, error) {
	index := ScriptIndex{}
	if wanted != nil && len(wanted) == 0 {
		return index, nil
	}
	guidIndex, err := BuildGUIDIndexForGUIDs(p, wanted)
	if err != nil {
		return nil, err
	}
	for guid, path := range guidIndex {
		if strings.HasSuffix(path, ".cs") {
			index[guid] = path
		}
	}
	return index, nil
}

func containsLower(value, lowerNeedle string) bool {
	return strings.Contains(strings.ToLower(value), lowerNeedle)
}

func BuildGUIDIndex(p Project) (GUIDIndex, error) {
	return BuildGUIDIndexForGUIDs(p, nil)
}

func BuildGUIDIndexForGUIDs(p Project, wanted map[string]bool) (GUIDIndex, error) {
	index := GUIDIndex{}
	if wanted != nil && len(wanted) == 0 {
		return index, nil
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > 16 {
		workers = 16
	}

	type item struct {
		guid string
		path string
	}
	jobs := make(chan string, workers*4)
	results := make(chan item, workers*4)
	done := make(chan struct{})
	var closeDone sync.Once
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				case path, ok := <-jobs:
					if !ok {
						return
					}
					guid := ReadMetaGUID(path)
					if guid == "" {
						continue
					}
					guid = strings.ToLower(guid)
					if wanted != nil && !wanted[guid] {
						continue
					}
					result := item{guid: guid, path: p.AssetPath(strings.TrimSuffix(path, ".meta"))}
					select {
					case results <- result:
					case <-done:
						return
					}
				}
			}
		}()
	}
	walkErr := make(chan error, 1)
	go func() {
		walkErr <- filepath.WalkDir(p.Assets, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			select {
			case <-done:
				return filepath.SkipAll
			default:
			}
			if d.IsDir() {
				if shouldSkipDir(d.Name()) && path != p.Assets {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".meta") {
				return nil
			}
			select {
			case jobs <- path:
			case <-done:
				return filepath.SkipAll
			}
			return nil
		})
		close(jobs)
		wg.Wait()
		close(results)
	}()

	for result := range results {
		index[result.guid] = result.path
		if wanted != nil && len(index) == len(wanted) {
			closeDone.Do(func() { close(done) })
		}
	}
	if err := <-walkErr; err != nil && err != filepath.SkipAll {
		return nil, err
	}
	return index, nil
}

func ReadMetaGUID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	start := 0
	if !bytes.HasPrefix(data, []byte("guid:")) {
		idx := bytes.Index(data, []byte("\nguid:"))
		if idx < 0 {
			return ""
		}
		start = idx + 1
	}
	start += len("guid:")
	end := bytes.IndexByte(data[start:], '\n')
	if end < 0 {
		end = len(data)
	} else {
		end += start
	}
	return strings.TrimSpace(string(data[start:end]))
}

func NativeClassName(classID int) string {
	return nativeClassNames[classID]
}

func MatchesNativeClassName(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	for _, name := range nativeClassNames {
		if containsLower(name, query) {
			return true
		}
	}
	return false
}

func scanObjectLine(obj *Object, line []byte) {
	if scanObjectTypeLine(obj, line) {
		return
	}
	if obj.Name == "" && bytes.HasPrefix(line, nameLinePrefix) {
		obj.Name = cleanScalar(string(bytes.TrimSpace(line[len(nameLinePrefix):])))
		return
	}
	if bytes.HasPrefix(line, gameObjectLinePrefix) {
		obj.GameObjectID = extractFileIDBytes(line)
		return
	}
	if bytes.HasPrefix(line, sourceObjectLinePrefix) {
		obj.SourceObjectID = extractFileIDBytes(line)
		obj.SourceGUID = extractGUIDBytes(line)
		return
	}
	if bytes.HasPrefix(line, fatherLinePrefix) {
		obj.FatherTransformID = extractFileIDBytes(line)
		return
	}
	if obj.Type == "GameObject" && bytes.Contains(line, componentLineMarker) {
		if id := extractFileIDBytes(line); id != "" && id != "0" {
			obj.ComponentIDs = append(obj.ComponentIDs, id)
		}
		return
	}
	if obj.Type == "MonoBehaviour" && bytes.HasPrefix(line, scriptLinePrefix) {
		obj.ScriptGUID = extractGUIDBytes(line)
	}
}

func (a *Asset) applyPrefabNameOverrides() {
	names := map[string]string{}
	for _, override := range a.PrefabOverrides() {
		if override.Kind != "property" || override.PropertyPath != "m_Name" || override.Value == "" {
			continue
		}
		id := extractFileID(override.Target)
		guid := extractGUID(override.Target)
		if id == "" || guid == "" {
			continue
		}
		names[strings.ToLower(guid)+"\x00"+id] = override.Value
	}
	if len(names) == 0 {
		return
	}
	for _, obj := range a.Objects {
		if obj.Name != "" || obj.SourceObjectID == "" || obj.SourceGUID == "" {
			continue
		}
		if name := names[strings.ToLower(obj.SourceGUID)+"\x00"+obj.SourceObjectID]; name != "" {
			obj.Name = name
		}
	}
}

func scanObjectTypeLine(obj *Object, line []byte) bool {
	if obj.Type != "" || len(line) <= 1 || line[0] == ' ' || line[len(line)-1] != ':' {
		return false
	}
	obj.Type = strings.TrimSpace(string(line[:len(line)-1]))
	return true
}

func parseHeaderLine(line []byte) (int, string, bool) {
	prefix := []byte("--- !u!")
	if !bytes.HasPrefix(line, prefix) {
		return 0, "", false
	}
	rest := line[len(prefix):]
	space := bytes.IndexByte(rest, ' ')
	if space <= 0 || space+2 > len(rest) || rest[space+1] != '&' {
		return 0, "", false
	}
	classID, ok := parsePositiveInt(rest[:space])
	if !ok {
		return 0, "", false
	}
	id := string(rest[space+2:])
	if fields := strings.Fields(id); len(fields) > 0 {
		id = fields[0]
	}
	if id == "" {
		return 0, "", false
	}
	return classID, id, true
}

func parsePositiveInt(value []byte) (int, bool) {
	if len(value) == 0 {
		return 0, false
	}
	out := 0
	for _, b := range value {
		if b < '0' || b > '9' {
			return 0, false
		}
		out = out*10 + int(b-'0')
	}
	return out, true
}

func readScalar(lines []string, key string) string {
	prefix := "  " + key + ":"
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return cleanScalar(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
		}
	}
	return ""
}

func readFileIDField(lines []string, key string) string {
	prefix := "  " + key + ":"
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return extractFileID(line)
		}
	}
	return ""
}

func readGUIDField(lines []string, key string) string {
	prefix := "  " + key + ":"
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return extractGUID(line)
		}
	}
	return ""
}

func readComponentIDs(lines []string) []string {
	var ids []string
	for _, line := range lines {
		if strings.Contains(line, "- component:") {
			if id := extractFileID(line); id != "" && id != "0" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func extractFileID(line string) string {
	start := strings.Index(line, "fileID:")
	if start < 0 {
		return ""
	}
	i := start + len("fileID:")
	for i < len(line) && line[i] == ' ' {
		i++
	}
	begin := i
	if i < len(line) && line[i] == '-' {
		i++
	}
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == begin || (line[begin] == '-' && i == begin+1) {
		return ""
	}
	return line[begin:i]
}

func extractFileIDBytes(line []byte) string {
	start := bytes.Index(line, []byte("fileID:"))
	if start < 0 {
		return ""
	}
	i := start + len("fileID:")
	for i < len(line) && line[i] == ' ' {
		i++
	}
	begin := i
	if i < len(line) && line[i] == '-' {
		i++
	}
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == begin || (line[begin] == '-' && i == begin+1) {
		return ""
	}
	return string(line[begin:i])
}

func extractGUID(line string) string {
	start := strings.Index(line, "guid:")
	if start < 0 {
		return ""
	}
	return scanGUID(line[start+len("guid:"):])
}

func extractGUIDBytes(line []byte) string {
	start := bytes.Index(line, []byte("guid:"))
	if start < 0 {
		return ""
	}
	return scanGUIDBytes(line[start+len("guid:"):])
}

func findGUIDs(value string) []string {
	var out []string
	for offset := 0; offset < len(value); {
		start := strings.Index(value[offset:], "guid:")
		if start < 0 {
			break
		}
		offset += start + len("guid:")
		guid := scanGUID(value[offset:])
		if guid != "" {
			out = append(out, guid)
			offset += len(guid)
		}
	}
	return out
}

func scanGUID(value string) string {
	i := 0
	for i < len(value) && value[i] == ' ' {
		i++
	}
	start := i
	for i < len(value) && isHex(value[i]) {
		i++
	}
	if i == start {
		return ""
	}
	return strings.ToLower(value[start:i])
}

func scanGUIDBytes(value []byte) string {
	i := 0
	for i < len(value) && value[i] == ' ' {
		i++
	}
	start := i
	for i < len(value) && isHex(value[i]) {
		i++
	}
	if i == start {
		return ""
	}
	return strings.ToLower(string(value[start:i]))
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'f') ||
		(b >= 'A' && b <= 'F')
}

func cleanScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		return strings.Trim(value, `"`)
	}
	return value
}

func skipField(key string) bool {
	switch key {
	case "m_Name", "m_ObjectHideFlags", "m_CorrespondingSourceObject", "m_PrefabInstance",
		"m_PrefabAsset", "serializedVersion", "m_GameObject", "m_Script",
		"m_Enabled", "m_EditorHideFlags", "m_EditorClassIdentifier", "m_Modification":
		return true
	default:
		return false
	}
}

func sortNodes(nodes []*Node) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].GameObject.Order < nodes[j].GameObject.Order
	})
	for _, node := range nodes {
		sortNodes(node.Children)
	}
}

func assignNodePath(node *Node, parent string, depth int) {
	node.Depth = depth
	name := node.GameObject.Name
	if name == "" {
		name = "<unnamed:" + node.GameObject.ID + ">"
	}
	if parent == "" {
		node.Path = name
	} else {
		node.Path = parent + "/" + name
	}
	for _, child := range node.Children {
		assignNodePath(child, node.Path, depth+1)
	}
}

func shortGUID(guid string) string {
	if len(guid) <= 8 {
		return guid
	}
	return guid[:8]
}

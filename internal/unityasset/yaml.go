package unityasset

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	headerRE = regexp.MustCompile(`^--- !u!(\d+) &(-?\d+)`)
	fileIDRE = regexp.MustCompile(`fileID:\s*(-?\d+)`)
	guidRE   = regexp.MustCompile(`guid:\s*([0-9a-fA-F]+)`)
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

type Asset struct {
	Path        string
	Kind        string
	GUID        string
	Objects     []*Object
	ByID        map[string]*Object
	ScriptIndex ScriptIndex
	GUIDIndex   GUIDIndex
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

type ScriptIndex map[string]string

type GUIDIndex map[string]string

func ReadAsset(p Project, entry FileEntry, scripts ScriptIndex) (*Asset, error) {
	data, err := os.ReadFile(entry.Abs)
	if err != nil {
		return nil, err
	}
	asset, err := ParseAsset(data)
	if err != nil {
		return nil, err
	}
	asset.Path = entry.AssetPath
	asset.Kind = entry.Kind
	asset.GUID = ReadMetaGUID(entry.Abs + ".meta")
	asset.ScriptIndex = scripts
	return asset, nil
}

func ParseAsset(data []byte) (*Asset, error) {
	asset := &Asset{ByID: map[string]*Object{}}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024*16)

	var current *Object
	for scanner.Scan() {
		line := scanner.Text()
		if m := headerRE.FindStringSubmatch(line); m != nil {
			if current != nil {
				asset.finishObject(current)
			}
			classID, _ := strconv.Atoi(m[1])
			current = &Object{
				ID:      m[2],
				ClassID: classID,
				Order:   len(asset.Objects),
			}
			continue
		}
		if current == nil {
			continue
		}
		current.Lines = append(current.Lines, line)
		if current.Type == "" && strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " ") {
			current.Type = strings.TrimSuffix(strings.TrimSpace(line), ":")
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
	return asset, nil
}

func (a *Asset) finishObject(o *Object) {
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
	if o.Type == "MonoBehaviour" {
		o.ScriptGUID = readGUIDField(o.Lines, "m_Script")
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

func (a *Asset) ComponentsFor(goID string) []Component {
	goObj := a.ByID[goID]
	if goObj == nil {
		return nil
	}
	out := make([]Component, 0, len(goObj.ComponentIDs))
	for _, compID := range goObj.ComponentIDs {
		compObj := a.ByID[compID]
		if compObj == nil {
			continue
		}
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

	nodes := map[string]*Node{}
	for _, goObj := range a.GameObjects() {
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
	for _, goObj := range a.GameObjects() {
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
	if limit <= 0 {
		limit = 20
	}
	fields := make([]Field, 0, limit)
	hidden := 0
	for i := 0; i < len(obj.Lines); i++ {
		line := obj.Lines[i]
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") {
			continue
		}
		if strings.HasPrefix(line, "  -") {
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
			value = summarizeNested(obj.Lines, i+1)
		}
		value = a.ResolveReferences(value)
		if len(fields) >= limit {
			hidden++
			continue
		}
		fields = append(fields, Field{Name: key, Value: value})
	}
	return fields, hidden
}

func (a *Asset) ResolveReferences(value string) string {
	if len(a.GUIDIndex) == 0 {
		return value
	}
	matches := guidRE.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return value
	}

	paths := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		guid := strings.ToLower(match[1])
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
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "  -") {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		trim = strings.TrimPrefix(trim, "- ")
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

func BuildScriptIndexForGUIDs(p Project, wanted map[string]bool) (ScriptIndex, error) {
	index := ScriptIndex{}
	if wanted != nil && len(wanted) == 0 {
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
		guid := ReadMetaGUID(path)
		if guid == "" {
			return nil
		}
		if wanted != nil && !wanted[strings.ToLower(guid)] {
			return nil
		}
		scriptPath := strings.TrimSuffix(path, ".meta")
		index[strings.ToLower(guid)] = p.AssetPath(scriptPath)
		if wanted != nil && len(index) == len(wanted) {
			return filepath.SkipAll
		}
		return nil
	})
	return index, err
}

func BuildGUIDIndex(p Project) (GUIDIndex, error) {
	index := GUIDIndex{}
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
		if !strings.HasSuffix(path, ".meta") {
			return nil
		}
		guid := ReadMetaGUID(path)
		if guid == "" {
			return nil
		}
		assetPath := strings.TrimSuffix(path, ".meta")
		index[strings.ToLower(guid)] = p.AssetPath(assetPath)
		return nil
	})
	return index, err
}

func ReadMetaGUID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "guid:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "guid:"))
		}
	}
	return ""
}

func NativeClassName(classID int) string {
	return nativeClassNames[classID]
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
	m := fileIDRE.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1]
}

func extractGUID(line string) string {
	m := guidRE.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

func cleanScalar(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"`)
	return value
}

func skipField(key string) bool {
	switch key {
	case "m_Name", "m_ObjectHideFlags", "m_CorrespondingSourceObject", "m_PrefabInstance",
		"m_PrefabAsset", "serializedVersion", "m_GameObject", "m_Script",
		"m_Enabled", "m_EditorHideFlags", "m_EditorClassIdentifier":
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
		name = "<unnamed>"
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

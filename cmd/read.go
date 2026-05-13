package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/youngwoocho02/unity-scanner/internal/format"
	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

type readOptions struct {
	commonOptions
	depth      int
	path       string
	component  string
	fieldLimit int
	limit      int
	fullTree   bool
}

func readCmd(args []string) error {
	opts := readOptions{depth: -1}
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	addCommonFlags(fs, &opts.commonOptions)
	fs.IntVar(&opts.depth, "depth", opts.depth, "hierarchy depth")
	fs.StringVar(&opts.path, "path", "", "object path/name filter")
	fs.StringVar(&opts.component, "component", "", "component filter")
	fs.IntVar(&opts.fieldLimit, "field-limit", opts.fieldLimit, "field limit")
	fs.IntVar(&opts.limit, "limit", opts.limit, "max GameObjects")
	fs.BoolVar(&opts.fullTree, "full-tree", false, "show every visible tree row without render-only folding")
	if err := parse(fs, args); err != nil {
		if err == flag.ErrHelp {
			printTopicHelp(os.Stdout, "read")
			return nil
		}
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("read requires an asset path")
	}

	project, err := unityasset.OpenProject(opts.project)
	if err != nil {
		return err
	}
	abs, _, err := project.Resolve(fs.Arg(0))
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("read requires a file, got directory: %s", filepath.ToSlash(fs.Arg(0)))
	}
	result, err := unityasset.Scan(project, fs.Arg(0), unityasset.ScanOptions{})
	if err != nil {
		return err
	}
	if len(result.Files) == 0 {
		return fmt.Errorf("asset not found: %s", fs.Arg(0))
	}
	entry := result.Files[0]
	if !unityasset.KnownUnityYAMLKind(entry.Kind) {
		return fmt.Errorf("read supports Unity YAML assets, got %s", entry.Kind)
	}

	asset, err := unityasset.ReadAsset(entry, unityasset.ScriptIndex{})
	if err != nil {
		return err
	}
	if entry.Kind == "prefab" {
		sourceGUIDs := sourceGUIDSet(asset.SourcePrefabGUIDs())
		if len(sourceGUIDs) > 0 {
			index, err := unityasset.BuildGUIDIndexForGUIDs(project, sourceGUIDs)
			if err != nil {
				return err
			}
			asset.SourcePaths = sourcePaths(asset.SourcePrefabGUIDs(), index)
		}
	}
	scripts, err := unityasset.BuildScriptIndexForGUIDs(project, asset.ScriptGUIDs())
	if err != nil {
		return err
	}
	asset.ScriptIndex = scripts
	roots := asset.Hierarchy()
	flat := flattenHierarchy(roots)
	components := buildReadComponentView(asset, flat)
	if fieldGUIDs := fieldReferenceGUIDs(asset, flat, components, opts); len(fieldGUIDs) > 0 {
		guidIndex, err := unityasset.BuildGUIDIndexForGUIDs(project, fieldGUIDs)
		if err != nil {
			return err
		}
		asset.GUIDIndex = guidIndex
	}
	printRead(asset, roots, flat, components, opts)
	return nil
}

type readComponentView struct {
	byGO  map[string][]unityasset.Component
	count int
	names []string
}

func buildReadComponentView(asset *unityasset.Asset, nodes []*unityasset.Node) readComponentView {
	view := readComponentView{byGO: map[string][]unityasset.Component{}}
	seenNames := map[string]bool{}
	for _, node := range nodes {
		components := asset.ComponentsFor(node.GameObject.ID)
		view.byGO[node.GameObject.ID] = components
		view.count += len(components)
		for _, component := range components {
			if seenNames[component.Name] {
				continue
			}
			seenNames[component.Name] = true
			view.names = append(view.names, component.Name)
		}
	}
	return view
}

func (v readComponentView) components(goID string) []unityasset.Component {
	return v.byGO[goID]
}

func fieldReferenceGUIDs(asset *unityasset.Asset, nodes []*unityasset.Node, components readComponentView, opts readOptions) map[string]bool {
	guids := map[string]bool{}
	if asset.Kind == "asset" {
		addObjectVisibleFieldGUIDs(guids, asset.Objects, opts.fieldLimit)
		return guids
	}
	if opts.component == "" {
		return guids
	}
	for _, node := range nodes {
		if opts.path != "" && !containsFold(node.Path, opts.path) {
			continue
		}
		for _, component := range components.components(node.GameObject.ID) {
			if containsFold(component.Name, opts.component) {
				unityasset.AddVisibleFieldGUIDs(guids, component.Object, opts.fieldLimit)
			}
		}
	}
	return guids
}

func addObjectVisibleFieldGUIDs(guids map[string]bool, objects []*unityasset.Object, limit int) {
	for _, obj := range objects {
		unityasset.AddVisibleFieldGUIDs(guids, obj, limit)
	}
}

func printRead(asset *unityasset.Asset, roots []*unityasset.Node, flat []*unityasset.Node, components readComponentView, opts readOptions) {
	fmt.Printf("ASSET       %s\n", asset.Kind)
	fmt.Printf("PATH        %s\n", asset.Path)
	if asset.GUID != "" {
		fmt.Printf("GUID        %s\n", asset.GUID)
	}
	if opts.component == "" && len(asset.SourcePaths) > 0 {
		printPathGroupSection("PREFAB_SOURCES", asset.SourcePaths, opts.lineWidth)
	}
	if len(flat) == 0 {
		fmt.Printf("YAML_OBJECTS %d\n", len(asset.Objects))
	} else {
		fmt.Printf("OBJECTS     %d\n", len(flat))
		fmt.Printf("COMPONENTS  %d\n", components.count)
		if opts.component == "" && opts.depth >= 0 {
			fmt.Printf("DEPTH       %d\n", opts.depth)
		}
	}
	fmt.Println()

	if len(flat) == 0 {
		printYAMLObjects(asset, opts)
		return
	}
	if opts.component != "" {
		printComponentRead(asset, flat, components, opts)
		return
	}

	printHierarchy(roots, components, opts)
}

func flattenHierarchy(roots []*unityasset.Node) []*unityasset.Node {
	var out []*unityasset.Node
	var walk func(nodes []*unityasset.Node)
	walk = func(nodes []*unityasset.Node) {
		for _, node := range nodes {
			out = append(out, node)
			walk(node.Children)
		}
	}
	walk(roots)
	return out
}

func printHierarchy(roots []*unityasset.Node, components readComponentView, opts readOptions) {
	rows, hidden := collectHierarchyRows(roots, components, opts)
	if len(rows) == 0 {
		if opts.path != "" {
			printfLineLimited(opts.lineWidth, "no object path matched %q", opts.path)
			return
		}
	}
	focusRows, focusHidden := limitFocusRows(rows, opts.limit)
	treeRows, limitHidden := limitTreeRows(rows, opts.limit)
	hidden += limitHidden
	printComponentSets(mergeRows(treeRows, focusRows))
	printFocusRows(focusRows, focusHidden)
	fmt.Println("TREE")
	collapsed := printTreeRows(treeRows, opts)
	if hidden > 0 {
		fmt.Printf("\nmore: %d hidden by depth/limit\n", hidden)
	}
	if collapsed > 0 {
		fmt.Printf("collapsed render-only: %d\n", collapsed)
	}
	if opts.path == "" && (hidden > 0 || collapsed > 0) {
		fmt.Println("hint: use --depth N, --path NAME, --component NAME, or --full-tree")
	}
}

type hierarchyRow struct {
	Index        int
	Node         *unityasset.Node
	Components   []string
	ComponentSet string
	Focus        bool
	RenderOnly   bool
}

func collectHierarchyRows(roots []*unityasset.Node, components readComponentView, opts readOptions) ([]hierarchyRow, int) {
	rows := make([]hierarchyRow, 0)
	hidden := 0
	var walk func(nodes []*unityasset.Node)
	walk = func(nodes []*unityasset.Node) {
		for _, node := range nodes {
			if opts.path != "" && !containsFold(node.Path, opts.path) {
				childMatch := nodeHasPath(node, opts.path)
				if !childMatch {
					continue
				}
			}
			if opts.depth >= 0 && node.Depth > opts.depth {
				hidden += countNodes([]*unityasset.Node{node})
				continue
			}
			rows = append(rows, newHierarchyRow(components, len(rows), node))
			walk(node.Children)
		}
	}
	walk(roots)
	assignComponentSetCodes(rows)
	return rows, hidden
}

func limitTreeRows(rows []hierarchyRow, limit int) ([]hierarchyRow, int) {
	if limit <= 0 || len(rows) <= limit {
		return rows, 0
	}
	return rows[:limit], len(rows) - limit
}

func limitFocusRows(rows []hierarchyRow, limit int) ([]hierarchyRow, int) {
	focus := make([]hierarchyRow, 0)
	for _, row := range rows {
		if row.Focus {
			focus = append(focus, row)
		}
	}
	if limit <= 0 || len(focus) <= limit {
		return focus, 0
	}
	return focus[:limit], len(focus) - limit
}

func mergeRows(groups ...[]hierarchyRow) []hierarchyRow {
	seen := map[int]bool{}
	merged := make([]hierarchyRow, 0)
	for _, group := range groups {
		for _, row := range group {
			if seen[row.Index] {
				continue
			}
			seen[row.Index] = true
			merged = append(merged, row)
		}
	}
	return merged
}

func newHierarchyRow(components readComponentView, index int, node *unityasset.Node) hierarchyRow {
	componentList := components.components(node.GameObject.ID)
	names := make([]string, 0, len(componentList))
	for _, component := range componentList {
		names = append(names, component.Name)
	}
	return hierarchyRow{
		Index:      index,
		Node:       node,
		Components: names,
		Focus:      hasFocusComponent(names),
		RenderOnly: isRenderOnly(names),
	}
}

func assignComponentSetCodes(rows []hierarchyRow) {
	keyToCode := map[string]string{}
	for i := range rows {
		key := strings.Join(rows[i].Components, "\x00")
		if key == "" {
			continue
		}
		code := keyToCode[key]
		if code == "" {
			code = fmt.Sprintf("c%d", len(keyToCode)+1)
			keyToCode[key] = code
		}
		rows[i].ComponentSet = code
	}
}

func printFocusRows(rows []hierarchyRow, hidden int) {
	if len(rows) == 0 {
		return
	}
	fmt.Println("FOCUS")
	for _, row := range rows {
		fmt.Printf("  [%d] %s", row.Index, row.Node.Path)
		if row.ComponentSet != "" {
			fmt.Printf("  %s", row.ComponentSet)
		}
		fmt.Println()
	}
	if hidden > 0 {
		fmt.Printf("  more focus: %d hidden by --limit\n", hidden)
	}
	fmt.Println()
}

func printTreeRows(rows []hierarchyRow, opts readOptions) int {
	if opts.fullTree {
		for _, row := range rows {
			printTreeRow(row)
		}
		return 0
	}

	collapsed := 0
	for i := 0; i < len(rows); {
		groupEnd := collapsibleRunEnd(rows, i)
		if groupEnd-i >= 3 {
			printCollapsedRows(rows[i:groupEnd], opts.lineWidth)
			collapsed += groupEnd - i
			i = groupEnd
			continue
		}
		printTreeRow(rows[i])
		i++
	}
	return collapsed
}

func collapsibleRunEnd(rows []hierarchyRow, start int) int {
	first := rows[start]
	if !canCollapseRow(first) {
		return start + 1
	}
	end := start + 1
	for end < len(rows) && canCollapseRow(rows[end]) &&
		rows[end].Node.Depth == first.Node.Depth &&
		rows[end].ComponentSet == first.ComponentSet {
		end++
	}
	return end
}

func canCollapseRow(row hierarchyRow) bool {
	return row.RenderOnly && !row.Focus && len(row.Node.Children) == 0
}

func printTreeRow(row hierarchyRow) {
	indent := strings.Repeat("  ", row.Node.Depth)
	fmt.Printf("%s[%d] %s", indent, row.Index, displayObjectName(row.Node.GameObject))
	if row.ComponentSet != "" {
		fmt.Printf("  %s", row.ComponentSet)
	}
	fmt.Println()
}

func printCollapsedRows(rows []hierarchyRow, lineWidth int) {
	first := rows[0]
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, displayObjectName(row.Node.GameObject))
	}
	indent := strings.Repeat("  ", first.Node.Depth)
	printfLineLimited(lineWidth, "%s[%d..%d] %s  %s  (%d)", indent, rows[0].Index, rows[len(rows)-1].Index, strings.Join(format.CompressNames(names), ", "), first.ComponentSet, len(rows))
}

func hasFocusComponent(components []string) bool {
	for _, component := range components {
		if !isTrivialComponent(component) {
			return true
		}
	}
	return false
}

func isRenderOnly(components []string) bool {
	if len(components) == 0 {
		return false
	}
	hasRenderer := false
	for _, component := range components {
		if !isRenderComponent(component) {
			return false
		}
		if component != "Transform" && component != "RectTransform" {
			hasRenderer = true
		}
	}
	return hasRenderer
}

func isTrivialComponent(component string) bool {
	switch component {
	case "Transform", "RectTransform", "MeshFilter", "MeshRenderer",
		"SkinnedMeshRenderer", "SpriteRenderer", "CanvasRenderer", "LineRenderer":
		return true
	default:
		return false
	}
}

func isRenderComponent(component string) bool {
	return isTrivialComponent(component)
}

func printComponentSets(rows []hierarchyRow) {
	sets := map[string]string{}
	order := make([]string, 0)
	for _, row := range rows {
		if row.ComponentSet == "" {
			continue
		}
		key := row.ComponentSet
		value := strings.Join(row.Components, ", ")
		if _, ok := sets[key]; !ok {
			sets[key] = value
			order = append(order, key)
		}
	}
	if len(order) == 0 {
		return
	}
	fmt.Println("CMP")
	for _, key := range order {
		fmt.Printf("  %s %s\n", key, sets[key])
	}
	fmt.Println()
}

func printYAMLObjects(asset *unityasset.Asset, opts readOptions) {
	matches := 0
	for _, obj := range asset.Objects {
		name, scriptPath := asset.ComponentName(obj)
		if opts.component != "" && !containsFold(name, opts.component) && !containsFold(obj.Name, opts.component) {
			continue
		}
		matches++
		fmt.Printf("[%d] %s", obj.Order, name)
		if obj.Name != "" {
			fmt.Printf("  name: %s", obj.Name)
		}
		fmt.Println()
		if scriptPath != "" {
			fmt.Printf("    script: %s\n", scriptPath)
		}
		fields, hidden := asset.FieldsWithHidden(obj, opts.fieldLimit)
		for _, field := range fields {
			printfLineLimited(opts.lineWidth, "    %-24s %s", field.Name, field.Value)
		}
		if hidden > 0 {
			fmt.Printf("    more fields: %d hidden by --field-limit\n", hidden)
		}
	}
	if matches == 0 && opts.component != "" {
		fmt.Printf("no object matched %q\n", opts.component)
	}
}

func printComponentRead(asset *unityasset.Asset, nodes []*unityasset.Node, components readComponentView, opts readOptions) {
	matches := 0
	hidden := 0
	for _, node := range nodes {
		if opts.path != "" && !containsFold(node.Path, opts.path) {
			continue
		}
		for _, component := range components.components(node.GameObject.ID) {
			if !containsFold(component.Name, opts.component) {
				continue
			}
			if opts.limit > 0 && matches >= opts.limit {
				hidden++
				continue
			}
			matches++
			fmt.Printf("COMPONENT  %s\n", component.Name)
			fmt.Printf("OBJECT     %s\n", node.Path)
			if component.ScriptPath != "" {
				fmt.Printf("SCRIPT     %s\n", component.ScriptPath)
			}
			fields, hiddenFields := asset.FieldsWithHidden(component.Object, opts.fieldLimit)
			if len(fields) == 0 {
				fmt.Println("fields: <none>")
			} else {
				fmt.Println("fields:")
				for _, field := range fields {
					printfLineLimited(opts.lineWidth, "  %-24s %s", field.Name, field.Value)
				}
			}
			if hiddenFields > 0 {
				fmt.Printf("more fields: %d hidden by --field-limit\n", hiddenFields)
			}
			fmt.Println()
		}
	}
	if matches == 0 {
		fmt.Printf("no component matched %q\n", opts.component)
		printAvailableComponents(components, opts.lineWidth)
		printSourceHint(asset, opts.lineWidth)
	}
	if hidden > 0 {
		fmt.Printf("more components: %d hidden by --limit\n", hidden)
	}
}

func sourceGUIDSet(guids []string) map[string]bool {
	set := map[string]bool{}
	for _, guid := range guids {
		set[guid] = true
	}
	return set
}

func sourcePaths(guids []string, index unityasset.GUIDIndex) []string {
	seen := map[string]bool{}
	var out []string
	for _, guid := range guids {
		path := index[guid]
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func printSourceHint(asset *unityasset.Asset, lineWidth int) {
	if len(asset.SourcePaths) == 0 {
		return
	}
	printPathGroupSection("prefab sources:", asset.SourcePaths, lineWidth)
	fmt.Println("hint: read source prefabs to inspect nested or inherited components")
}

func displayObjectName(obj *unityasset.Object) string {
	if obj.Name != "" {
		return obj.Name
	}
	return "<unnamed:" + obj.ID + ">"
}

func printAvailableComponents(components readComponentView, lineWidth int) {
	if len(components.names) > 0 {
		printfLineLimited(lineWidth, "available: %s", strings.Join(components.names, ", "))
	}
}

func nodeHasPath(node *unityasset.Node, path string) bool {
	for _, child := range node.Children {
		if containsFold(child.Path, path) || nodeHasPath(child, path) {
			return true
		}
	}
	return false
}

func countNodes(nodes []*unityasset.Node) int {
	count := 0
	var walk func([]*unityasset.Node)
	walk = func(items []*unityasset.Node) {
		for _, item := range items {
			count++
			walk(item.Children)
		}
	}
	walk(nodes)
	return count
}

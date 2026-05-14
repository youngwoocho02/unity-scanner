# unity-scanner

[Korean](README.ko.md) | [Japanese](README.ja.md)

## Core Examples

- Remove path and extension noise: `Assets/Examples/Characters/Enemy_01.prefab` -> `Characters [prefab] Enemy_01`
- Omit `.meta` files: `Hero.prefab + Hero.prefab.meta` -> `Hero`
- Fold numbered names: `Enemy_01, Enemy_02, Enemy_03` -> `Enemy_01..03`
- Turn YAML objects into hierarchy: `GameObject + Transform + MeshRenderer` -> `TREE [0] HeroRoot c1`
- Code repeated component sets: `Transform, MeshFilter, MeshRenderer` x 40 -> `CMP c2 ...` + `... c2`
- Group repeated render objects: `SampleMesh_01 ... SampleMesh_08` -> `[2..9] SampleMesh_01..08 c2 (8)`
- Extract only needed fields: `MeshRenderer {40 fields}` -> `m_CastShadows, m_ReceiveShadows, more fields: 35 hidden`
- Resolve GUIDs to paths: `{guid: 222...}` -> `Assets/Examples/Data/SampleReference.asset`
- Structure match reasons: `SamplePanel.prefab:12 m_Name: SamplePanel` -> `[prefab] UI / SamplePanel / object: SamplePanel`
- Summarize GUID refs: `guid: 333...` x 30 -> `[asset] . :: SampleConfig`
- Show omitted counts: `hidden rows` -> `more: 41 hidden by depth/limit`
- Parallelize broad search: `name search 1500ms` -> `600ms`

## Why

1. Reduce token cost. Like RTK, reduce CLI output before it reaches the model by compressing repeated Unity paths, extensions, `.meta` files, GUIDs, and YAML fields.

2. Turn raw Unity YAML dumps into structured output. Instead of forcing the model to chase `GameObject`, `Transform`, components, fileIDs, and GUIDs separately, it shows hierarchy, component sets, and references together.

## Design

- CLI reads current files only: no cache, no required editor state.
- Optional Unity Editor package keeps changed and related assets serialized for file-based scans.
- Unity-aware compression: hierarchy, component groups, GUIDs, path groups.
- Compact by default: repeated data is declared once, omitted counts are shown.
- Parallel where useful: broad scans split work by file.
- Simple pipeline first: no extra wrappers, fallback layers, or features unless they reduce output or clarify Unity structure.

Token counts in examples below are approximate. They use `chars / 4` because exact tokens depend on the model tokenizer.

## Install

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-scanner/master/install.sh | sh
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/youngwoocho02/unity-scanner/master/install.ps1 | iex
```

The installer downloads the latest release binary and adds the install directory to `PATH`. After installing, run commands as `unity-scanner ...`.

### Unity Editor Package

Add via **Package Manager -> Add package from git URL**:

```text
https://github.com/youngwoocho02/unity-scanner.git?path=/unity-scanner-sync
```

Once added, the package watches changed assets, expands code and asset references, safely reserializes pending Unity YAML assets in small batches, and writes status under `Library/UnityScannerSync/`.

### Update

```bash
unity-scanner update
unity-scanner update --check
```

Update checks run only when `unity-scanner update` or `unity-scanner update --check` is called.

## Commands

```bash
unity-scanner list   -p <project> [path]
unity-scanner read   -p <project> <asset>
unity-scanner search -p <project> [path] [filters]
unity-scanner refs   -p <project> <asset-or-guid> [scan-path]
unity-scanner update [--check]
unity-scanner help [command]
unity-scanner version
```

Root options:

```text
-h, --help             Show help
-v, --version          Print version
```

Project command option:

```text
-p, --project <path>   Unity project path
--line-width <n>       Max output line width, default 1200, 0 disables truncation
--profile              Print command timing profile
--workers <n>          Parallel worker count, default CPU count
```

Command aliases: `ls` = `list`, `cat` = `read`, `find` = `search`.

## list

Compressed `ls` for Unity assets.

Without this tool:

```bash
find Assets/Examples -type f
```

```text
Assets/Examples/Characters/Hero.prefab
Assets/Examples/Characters/Hero.prefab.meta
Assets/Examples/Characters/Enemy_01.prefab
Assets/Examples/Characters/Enemy_01.prefab.meta
Assets/Examples/Data/HeroConfig.asset
Assets/Examples/Scenes/Demo.unity
Assets/Examples/Scripts/InventoryService.cs
Assets/Examples/UI/InventoryPanel.prefab
...
```

With `unity-scanner`:

```bash
unity-scanner list -p /projects/SampleProject Assets/Examples --depth 2 --limit 8
```

```text
EXT
  asset      .asset
  cs         .cs
  mat        .mat
  prefab     .prefab
  scene      .unity

DIRS
  Characters/ mat 2, prefab 5
  Data/       asset 4
  Scenes/     scene 2
  Scripts/    cs 3
  UI/         prefab 5

GROUPS
  Characters  [mat]
    HeroBody, HeroFace
  Characters  [prefab]
    Enemy_01..03, Hero, Villager
  Data  [asset]
    BalanceConfig, EnemyConfig, HeroConfig, ItemCatalog
  Scenes  [scene]
    Demo, Menu
  Scripts  [cs]
    CharacterController, InventoryService, UIHudController
  UI  [prefab]
    InventoryPanel, Tooltip, UI_Button_01..03
```

Difference:

```text
plain file list:      about 180 lines, 8400 chars, about 2100 tokens
unity-scanner list:   about  28 lines,  900 chars, about  225 tokens
reduction:            about 89% fewer chars
```

The command already contains the project and root path, so the output does not repeat them. Long `Assets/...` prefixes become groups, `.meta` files are omitted unless requested, and extensions are declared once in `EXT`.

## read

Model-context summary for Unity YAML assets: `.prefab`, `.unity`, `.asset`.

Without this tool:

```bash
cat Assets/Examples/Prefabs/SamplePrefab.prefab
```

```text
--- !u!1 &100001
GameObject:
  m_Name: SampleRoot
  m_Component:
  - component: {fileID: 400001}
  - component: {fileID: 230001}
--- !u!4 &400001
Transform:
  m_GameObject: {fileID: 100001}
  m_Father: {fileID: 0}
--- !u!23 &230001
MeshRenderer:
  m_GameObject: {fileID: 100001}
  m_CastShadows: 1
...
```

With `unity-scanner`:

```bash
unity-scanner read -p /projects/SampleProject Assets/Examples/Prefabs/SamplePrefab.prefab --depth 2 --limit 30
```

```text
ASSET       prefab
PATH        Assets/Examples/Prefabs/SamplePrefab.prefab
GUID        0123456789abcdef0123456789abcdef
OBJECTS     64
COMPONENTS  138
DEPTH       2

CMP
  c1 Transform
  c2 Transform, MeshFilter, MeshRenderer

TREE
[0] SampleRoot  c1
[1] SampleMeshRoot  c1
  [2..9] SampleMesh, SampleMesh_01..07  c2  (8)
[10] SampleLogicRoot  c1
[11] SampleChild  c1

more: 41 hidden by depth/limit
collapsed render-only: 18
hint: use --depth N, --path NAME, --component NAME, or --full-tree
```

Use `--full-tree` when you need every visible render-only row.

Difference:

```text
raw Unity YAML:       about 6000 lines, 200000 chars, about 50000 tokens
unity-scanner read:   about   30 lines,    900 chars, about   225 tokens
reduction:            about 99% fewer chars
```

Raw object blocks become a GameObject tree. Repeated component sets are declared in `CMP`. Render-only leaf repetition is folded, and hidden rows are counted.

### Component Drilldown

```bash
unity-scanner read -p /projects/SampleProject Assets/Examples/Prefabs/SamplePrefab.prefab --component MeshRenderer --path SampleMeshRoot --field-limit 3 --limit 3
```

Output shape:

```text
ASSET       prefab
PATH        Assets/Examples/Prefabs/SamplePrefab.prefab
GUID        0123456789abcdef0123456789abcdef
OBJECTS     64
COMPONENTS  138

COMPONENT  MeshRenderer
OBJECT     SampleRoot/SampleMeshRoot/SampleMesh
fields:
  m_CastShadows            1
  m_ReceiveShadows         1
  m_DynamicOccludee        1
more fields: 35 hidden by --field-limit

more components: 5 hidden by --limit
```

### ScriptableObject Asset

```bash
unity-scanner read -p /projects/SampleProject Assets/Examples/Data/SampleConfig.asset --field-limit 4
```

```text
ASSET       asset
PATH        Assets/Examples/Data/SampleConfig.asset
GUID        11111111111111111111111111111111
YAML_OBJECTS 1

[0] SampleConfig  name: SampleConfig
    script: Assets/Examples/Scripts/SampleConfig.cs
    startingLevel            1
    reference                {fileID: 11400000, guid: 22222222222222222222222222222222, type: 2} -> Assets/Examples/Data/SampleReference.asset
```

## search

Structured search for file names, GameObjects, components, and GUID references.

Without this tool:

```bash
grep -R -n -E "Sample|guid:" Assets/Examples/Prefabs
```

```text
Assets/Examples/Prefabs/Common/SamplePrefab.prefab:12:  m_Name: SampleRoot
Assets/Examples/Prefabs/Common/SamplePrefab.prefab:80:  m_Script: {fileID: 11500000, guid: 33333333333333333333333333333333, type: 3}
Assets/Examples/Prefabs/Common/SampleVariant.prefab:12:  m_Name: SampleRoot
Assets/Examples/Prefabs/UI/SamplePanel.prefab:12:  m_Name: SamplePanel
...
```

With `unity-scanner`:

```bash
unity-scanner search -p /projects/SampleProject Assets/Examples/Prefabs --name Sample --type prefab --limit 5
```

```text
EXT
  prefab     .prefab

MATCHES  3

[prefab] Common
  SamplePrefab
    file-name
  SampleVariant
    file-name
[prefab] UI
  SamplePanel
    file-name
```

Difference:

```text
plain grep/find:        about 40 lines, 2600 chars, about 650 tokens
unity-scanner search:   about 11 lines, 320 chars,  about 80 tokens
reduction:              about 80% fewer chars
```

Path and extension repetition are grouped. Name-only file hits stop at `file-name` instead of expanding YAML internals. Matches say whether the hit came from file name, GameObject, component, or GUID reference.

Broad searches use file-level parallelism when it helps. Example timing shape from a large Unity project:

```text
name search:      about 1500ms -> 600ms
guid search:      about 1600ms -> 1000ms
component search: about 2000ms -> 1100ms
```

## refs

Find where an asset or raw GUID is referenced.

Without this tool:

```bash
grep -R -n "33333333333333333333333333333333" Assets/Examples/Data
```

```text
Assets/Examples/Data/SampleConfig.asset:18:  m_Script: {fileID: 11500000, guid: 33333333333333333333333333333333, type: 3}
Assets/Examples/Data/SamplePreset.asset:44:  source: {fileID: 11400000, guid: 33333333333333333333333333333333, type: 2}
...
```

With `unity-scanner`:

```bash
unity-scanner refs -p /projects/SampleProject Assets/Examples/Scripts/SampleConfig.cs Assets/Examples/Data --limit 5
```

```text
REF     Assets/Examples/Scripts/SampleConfig.cs
GUID    33333333333333333333333333333333

EXT
  asset      .asset

MATCHES  1

[asset]
  . :: SampleConfig
```

`refs` accepts either an asset path or a raw 32-character GUID.

Difference:

```text
plain GUID grep:     about 30 lines, 2400 chars, about 600 tokens
unity-scanner refs:  about 10 lines, 260 chars,  about 65 tokens
reduction:           about 89% fewer chars
```

The target asset path is resolved once. Results are grouped by asset type and folder instead of repeating raw YAML reference lines.

## Options

### list

```text
--depth <n>       directory summary depth, default unlimited
--kind <list>     comma-separated kinds: prefab,scene,asset,cs,mat
--meta            include .meta files in body
--flat            omit directory summary
--limit <n>       max groups, default unlimited
```

### read

```text
--depth <n>          hierarchy depth, default unlimited
--path <name/path>   only show matching object branch
--component <name>   show fields for matching component; prefab local misses search source prefabs
--id <fileID>        focus a local YAML object/component by fileID
--field-limit <n>    max fields per component, default unlimited
--limit <n>          max GameObjects/component matches, default unlimited
--full-tree          show every visible tree row without render-only folding
--override <text>    only show prefab overrides matching text
--override-limit <n> max prefab overrides shown, default 40, 0 unlimited
--raw-overrides      show raw prefab override target references
--ref-format <mode>  field reference format: name, path, or raw, default name
--no-resolve         skip script, GUID, and source prefab path resolution
```

When `read` reports `PREFAB_SOURCES`, the tree view is local serialized YAML. `read --component` searches source prefabs on local misses and prints `SOURCE_MATCHES`. Source matches are marked `INHERITED`, and matching variant property overrides are printed separately as `variant overrides`. Use Unity `LoadPrefabContents` only when the fully editor-resolved prefab state matters.

### search

```text
--name <text>        match file or GameObject name
--component <text>   match component/script name
--script-path <path> match MonoBehaviour scripts under asset path
--source <text>      match prefab source path/name
--guid <guid>        match raw Unity GUID reference
--ref <guid>         alias of --guid
--type <list>        prefab,scene,asset,cs,mat
--compact            one-line grouped result
--warnings <mode>    summary or detail, default summary
--limit <n>          max result files, default unlimited
--object-limit <n>   max objects shown per result file, default 12
```

### refs

```text
--type <list>        prefab,scene,asset,mat,controller
--detail             print detailed matches instead of compact groups
--warnings <mode>    summary or detail, default summary
--limit <n>          max result files, default unlimited
```

### update

```text
--check              check for updates without installing
```

## Design Choices

### No Cache

Cache would make repeated scans faster, but it adds invalidation rules and stale-result risk. This tool stays simple: command in, current files read, compact result out.

### No Required Editor Connection

Unity Editor can provide richer type data, but then the tool depends on an open project, a connector, and editor state. `unity-scanner` stays offline by default; the optional `unity-scanner-sync` package only keeps related YAML files fresh for later CLI reads.

### Compact Before Complete

For agent workflows, the first answer should usually be a map, not a dump. Use `list` or `search` to find the likely target, then `read --component` or `refs --detail` to drill in.

## Development

```bash
gofmt -w .
go test ./...
go build -o unity-scanner .
```

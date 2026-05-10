# unity-scanner

[英語](README.md) | [韓国語](README.ko.md)

## 主な例

- path と拡張子を削る: `Assets/Examples/Characters/Enemy_01.prefab` -> `Characters [prefab] Enemy_01`
- `.meta` file を省く: `Hero.prefab + Hero.prefab.meta` -> `Hero`
- 連番名を畳む: `Enemy_01, Enemy_02, Enemy_03` -> `Enemy_01..03`
- YAML object を階層に変える: `GameObject + Transform + MeshRenderer` -> `TREE [0] HeroRoot c1`
- 繰り返し component set をコード化: `Transform, MeshFilter, MeshRenderer` x 40 -> `CMP c2 ...` + `... c2`
- 同じ render object をまとめる: `SampleMesh_01 ... SampleMesh_08` -> `[2..9] SampleMesh_01..08 c2 (8)`
- 必要な field だけ抜く: `MeshRenderer {40 fields}` -> `m_CastShadows, m_ReceiveShadows, more fields: 35 hidden`
- GUID を path に解決する: `{guid: 222...}` -> `Assets/Examples/Data/SampleReference.asset`
- 一致理由を構造化する: `SamplePanel.prefab:12 m_Name: SamplePanel` -> `[prefab] UI / SamplePanel / object: SamplePanel`
- GUID ref を要約する: `guid: 333...` x 30 -> `[asset] . :: SampleConfig`
- 省略数を出す: `hidden rows` -> `more: 41 hidden by depth/limit`
- 広い検索を並列化する: `name search 1500ms` -> `600ms`

## なぜ作ったか

1. トークンコストを減らす。RTK と同じく、モデルに入る前に Unity の繰り返し path、拡張子、`.meta`、GUID、YAML field を圧縮する。

2. Unity YAML の raw dump を構造化出力に変える。`GameObject`、`Transform`、component、fileID、GUID を別々に追わせず、階層、component set、参照関係をまとめて表示する。

## 設計

- 現在のファイルだけ読む。cache なし、Editor state 依存なし
- Unity 構造を使う。階層、コンポーネントグループ、GUID、パスグループ
- デフォルト出力は圧縮優先。繰り返し情報は一度だけ宣言し、省略数は表示
- 大きな scan はファイル単位で並列処理
- 単純な pipeline を優先。出力削減や Unity 構造の解釈に直接効かない wrapper、fallback、機能は追加しない

下の例のトークン数は正確なトークナイザー結果ではなく、`文字数 / 4` の概算。実際のトークン数はモデルによって変わる。

## インストール

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-scanner/master/install.sh | sh
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/youngwoocho02/unity-scanner/master/install.ps1 | iex
```

installer は latest release binary を download し、install directory を `PATH` に追加する。install 後の command は `unity-scanner ...` で実行する。

### update

```bash
unity-scanner update
unity-scanner update --check
```

update 確認は `unity-scanner update` または `unity-scanner update --check` を実行した時だけ行う。

## コマンド

```bash
unity-scanner list   -p <project> [path]
unity-scanner read   -p <project> <asset>
unity-scanner search -p <project> [path] [filters]
unity-scanner refs   -p <project> <asset-or-guid> [scan-path]
unity-scanner update [--check]
```

共通 option:

```text
-p, --project <path>   Unity project path
```

## list

Unity asset 用の圧縮 `ls`

このツールなしの場合:

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

`unity-scanner` の場合:

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

差:

```text
通常のファイル一覧:   約 180行, 8400文字, 約 2100トークン
unity-scanner list: 約  28行,  900文字, 約  225トークン
削減:               文字数で約 89%
```

project と root path はすでに command にあるため、出力では繰り返さない。長い `Assets/...` prefix は group にまとめ、`.meta` は要求されない限り省き、拡張子は `EXT` で一度だけ宣言する。

## read

Unity YAML asset を model context 向けの構造に要約する。対象は `.prefab`, `.unity`, `.asset`

このツールなしの場合:

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

`unity-scanner` の場合:

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

render-only 行をすべて見たい場合は `--full-tree` を使う。

差:

```text
未加工 Unity YAML:   約 6000行, 200000文字, 約 50000トークン
unity-scanner read: 約   30行,    900文字, 約   225トークン
削減:               文字数で約 99%
```

未加工の object block は GameObject tree に変わる。繰り返しの component set は `CMP` で一度だけ宣言する。render-only の繰り返しは折り畳み、隠した行数は表示する。

### Component Drilldown

```bash
unity-scanner read -p /projects/SampleProject Assets/Examples/Prefabs/SamplePrefab.prefab --component MeshRenderer --path SampleMeshRoot --field-limit 3 --limit 3
```

出力形:

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

file name, GameObject, Component, GUID reference を構造的に検索する。

このツールなしの場合:

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

`unity-scanner` の場合:

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

差:

```text
通常の grep/find:      約 40行, 2600文字, 約 650トークン
unity-scanner search: 約 11行,  320文字, 約 80トークン
削減:                 文字数で約 80%
```

path と拡張子の繰り返しは group で減らす。file name が name-only search に一致した場合、YAML 内部は展開せず `file-name` だけを表示する。一致理由は `file-name`, `object`, `components`, GUID reference のように構造化して表示する。

広い範囲の検索では効果がある場合に file 単位で並列処理する。

```text
name search:      約 1500ms -> 600ms
guid search:      約 1600ms -> 1000ms
component search: 約 2000ms -> 1100ms
```

## refs

特定 asset または raw GUID がどこで参照されているかを探す。

このツールなしの場合:

```bash
grep -R -n "33333333333333333333333333333333" Assets/Examples/Data
```

```text
Assets/Examples/Data/SampleConfig.asset:18:  m_Script: {fileID: 11500000, guid: 33333333333333333333333333333333, type: 3}
Assets/Examples/Data/SamplePreset.asset:44:  source: {fileID: 11400000, guid: 33333333333333333333333333333333, type: 2}
...
```

`unity-scanner` の場合:

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

`refs` は asset path または 32 文字の raw GUID を受け取る。

差:

```text
通常の GUID grep:   約 30行, 2400文字, 約 600トークン
unity-scanner refs: 約 10行,  260文字, 約  65トークン
削減:               文字数で約 89%
```

対象 asset path は一度だけ解決する。結果は raw YAML reference line の繰り返しではなく、asset type と folder でまとまる。

## Options

### list

```text
--depth <n>       directory summary depth, default 2
--kind <list>     comma-separated kinds: prefab,scene,asset,cs,mat
--meta            include .meta files in body
--flat            omit directory summary
--limit <n>       max groups, default 80
```

### read

```text
--depth <n>          hierarchy depth, default 2
--path <name/path>   only show matching object branch
--component <name>   show fields for matching component
--field-limit <n>    max fields per component, default 20
--limit <n>          max GameObjects/component matches, default 60
--full-tree          show every visible tree row without render-only folding
```

### search

```text
--name <text>        match file or GameObject name
--component <text>   match component/script name
--guid <guid>        match raw Unity GUID reference
--ref <guid>         alias of --guid
--type <list>        prefab,scene,asset,cs,mat
--compact            one-line grouped result
--limit <n>          max result files, default 80
```

### refs

```text
--type <list>        prefab,scene,asset,mat,controller
--detail             print detailed matches instead of compact groups
--limit <n>          max result files, default 80
```

### update

```text
--check              install せず update だけ確認
```

## 設計上の選択

### キャッシュなし

cache は繰り返し scan を速くできるが、invalidation と stale result の問題が出る。この tool は単純に保つ。command を受け取り、現在の file を読み、compact な結果を出す。

### Editor 接続なし

Unity Editor に接続すればより豊富な type 情報を得られる。しかし open project、connector、Editor state に依存する。`unity-scanner` は意図的に offline tool として作る。

### 完全な dump より圧縮された map

agent workflow では最初の答えは raw dump より map の方が役に立つ。`list` や `search` で候補を絞り、`read --component` や `refs --detail` で深く見る。

## 開発検証

```bash
gofmt -w .
go test ./...
go build -o unity-scanner .
```

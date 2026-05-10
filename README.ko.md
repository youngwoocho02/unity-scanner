# unity-scanner

[영어](README.md) | [일본어](README.ja.md)

## 핵심 예시

- 경로와 확장자를 제거: `Assets/Examples/Characters/Enemy_01.prefab` -> `Characters [prefab] Enemy_01`
- `.meta` 파일을 생략: `Hero.prefab + Hero.prefab.meta` -> `Hero`
- 연속 이름을 축약: `Enemy_01, Enemy_02, Enemy_03` -> `Enemy_01..03`
- YAML 오브젝트를 계층으로 변환: `GameObject + Transform + MeshRenderer` -> `TREE [0] HeroRoot c1`
- 반복 컴포넌트 조합을 코드화: `Transform, MeshFilter, MeshRenderer` x 40 -> `CMP c2 ...` + `... c2`
- 같은 렌더 오브젝트를 묶기: `SampleMesh_01 ... SampleMesh_08` -> `[2..9] SampleMesh_01..08 c2 (8)`
- 필요한 필드만 추출: `MeshRenderer {40 fields}` -> `m_CastShadows, m_ReceiveShadows, more fields: 35 hidden`
- GUID를 경로로 해석: `{guid: 222...}` -> `Assets/Examples/Data/SampleReference.asset`
- 검색 이유를 구조화: `SamplePanel.prefab:12 m_Name: SamplePanel` -> `[prefab] UI / SamplePanel / object: SamplePanel`
- GUID 참조를 요약: `guid: 333...` x 30 -> `[asset] . :: SampleConfig`
- 생략량을 표시: `hidden rows` -> `more: 41 hidden by depth/limit`
- 넓은 검색을 병렬화: `name search 1500ms` -> `600ms`

## 왜 만들었나

1. 토큰 비용을 줄인다. RTK처럼 모델에 들어가기 전 CLI 출력을 줄이고, Unity에서 반복되는 경로, 확장자, `.meta`, GUID, YAML 필드를 압축한다.

2. Unity YAML 원문 덤프를 구조 출력으로 바꾼다. `GameObject`, `Transform`, 컴포넌트, fileID, GUID를 각각 따라가게 하지 않고, 계층, 컴포넌트 조합, 참조 관계를 한 번에 보여준다.

## 설계

- 현재 파일만 읽음. 캐시 없음, Editor 상태 의존 없음
- Unity 구조 활용. 계층, 컴포넌트 그룹, GUID, 경로 그룹
- 기본 출력은 압축 우선. 반복 정보는 한 번만 선언하고 생략 개수는 표시
- 큰 스캔은 파일 단위 병렬 처리

아래 예시의 토큰 수는 정확한 토크나이저 결과가 아니라 `글자 수 / 4` 기준의 대략값이다. 모델마다 실제 토큰 수는 달라질 수 있다.

## 설치

```bash
go build -o unity-scanner .
```

레포 루트에서 실행하거나 바이너리를 `PATH`에 넣으면 된다.

```bash
./unity-scanner list -p /projects/SampleProject Assets
```

## 명령

```bash
unity-scanner list   -p <project> [path]
unity-scanner read   -p <project> <asset>
unity-scanner search -p <project> [path] [filters]
unity-scanner refs   -p <project> <asset-or-guid> [scan-path]
```

공통 옵션:

```text
-p, --project <path>   Unity project path
```

## list

Unity 에셋용 압축 `ls`

이 도구 없이 하면:

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

`unity-scanner`로 하면:

```bash
./unity-scanner list -p /projects/SampleProject Assets/Examples --depth 2 --limit 8
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

차이:

```text
일반 파일 목록:      약 180줄, 8400글자, 약 2100토큰
unity-scanner list: 약  28줄,  900글자, 약  225토큰
감소:               글자 기준 약 89%
```

프로젝트와 root 경로는 이미 명령에 들어 있으므로 출력에서 반복하지 않는다. 긴 `Assets/...` prefix는 그룹으로 줄이고, `.meta`는 요청하지 않으면 빼며, 확장자는 `EXT`에서 한 번만 선언한다.

## read

Unity YAML 에셋을 모델 컨텍스트용 구조로 요약한다. 대상은 `.prefab`, `.unity`, `.asset`

이 도구 없이 하면:

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

`unity-scanner`로 하면:

```bash
./unity-scanner read -p /projects/SampleProject Assets/Examples/Prefabs/SamplePrefab.prefab --depth 2 --limit 30
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

접힌 렌더 오브젝트를 전부 보려면 `--full-tree`를 붙인다.

차이:

```text
원본 Unity YAML:    약 6000줄, 200000글자, 약 50000토큰
unity-scanner read: 약   30줄,    900글자, 약   225토큰
감소:               글자 기준 약 99%
```

원본 오브젝트 블록은 GameObject 트리로 바뀐다. 반복 컴포넌트 조합은 `CMP`에 한 번만 선언한다. 렌더 전용 반복은 접고, 숨긴 행 수는 표시한다.

### Component Drilldown

```bash
./unity-scanner read -p /projects/SampleProject Assets/Examples/Prefabs/SamplePrefab.prefab --component MeshRenderer --path SampleMeshRoot --field-limit 3 --limit 3
```

출력 형태:

```text
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
./unity-scanner read -p /projects/SampleProject Assets/Examples/Data/SampleConfig.asset --field-limit 4
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

파일명, GameObject, Component, GUID 참조를 구조적으로 검색한다.

이 도구 없이 하면:

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

`unity-scanner`로 하면:

```bash
./unity-scanner search -p /projects/SampleProject Assets/Examples/Prefabs --name Sample --type prefab --limit 5
```

```text
EXT
  prefab     .prefab

MATCHES  3

[prefab] Common
  SamplePrefab
    file-name
    object: SampleRoot
    components: Transform
  SampleVariant
    file-name
    object: SampleRoot
    components: Transform
[prefab] UI
  SamplePanel
    file-name
```

차이:

```text
일반 grep/find:        약 40줄, 2600글자, 약 650토큰
unity-scanner search: 약 15줄,  520글자, 약 130토큰
감소:                  글자 기준 약 80%
```

경로와 확장자 반복은 그룹으로 줄인다. 매칭 이유는 `file-name`, `object`, `components`, GUID 참조처럼 구조적으로 표시한다.

넓은 범위 검색은 효과가 있을 때 파일 단위 병렬 처리를 사용한다.

```text
name search:      약 1500ms -> 600ms
guid search:      약 1600ms -> 1000ms
component search: 약 2000ms -> 1100ms
```

## refs

특정 에셋 또는 raw GUID가 어디서 참조되는지 찾는다.

이 도구 없이 하면:

```bash
grep -R -n "33333333333333333333333333333333" Assets/Examples/Data
```

```text
Assets/Examples/Data/SampleConfig.asset:18:  m_Script: {fileID: 11500000, guid: 33333333333333333333333333333333, type: 3}
Assets/Examples/Data/SamplePreset.asset:44:  source: {fileID: 11400000, guid: 33333333333333333333333333333333, type: 2}
...
```

`unity-scanner`로 하면:

```bash
./unity-scanner refs -p /projects/SampleProject Assets/Examples/Scripts/SampleConfig.cs Assets/Examples/Data --limit 5
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

`refs`는 에셋 경로나 32자 raw GUID를 받는다.

차이:

```text
일반 GUID grep:     약 30줄, 2400글자, 약 600토큰
unity-scanner refs: 약 10줄,  260글자, 약  65토큰
감소:               글자 기준 약 89%
```

대상 에셋 경로는 한 번만 해석한다. 결과는 raw YAML 참조 줄 반복이 아니라 에셋 타입과 폴더 기준으로 묶인다.

## 옵션

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

## 설계 선택

### 캐시 없음

캐시는 반복 스캔을 빠르게 만들 수 있지만, 무효화 규칙과 stale 결과 문제가 생긴다. 이 도구는 단순하게 유지한다. 명령을 받으면 현재 파일을 읽고, 압축된 결과를 출력한다.

### Editor 연결 없음

Unity Editor에 붙으면 더 풍부한 타입 정보를 얻을 수 있다. 대신 열린 프로젝트, connector, Editor 상태에 의존하게 된다. `unity-scanner`는 의도적으로 오프라인 도구다.

### 완전한 dump보다 압축 지도 우선

에이전트 작업에서 첫 출력은 원문 dump보다 지도가 더 유용하다. `list`나 `search`로 후보를 좁히고, `read --component`나 `refs --detail`로 더 들어간다.

## 개발 검증

```bash
gofmt -w .
go test ./...
go build -o unity-scanner .
```

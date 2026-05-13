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
- 단순 파이프라인 우선. 출력 감소나 Unity 구조 해석에 직접 도움 안 되는 래퍼, fallback, 기능은 추가하지 않음

아래 예시의 토큰 수는 정확한 토크나이저 결과가 아니라 `글자 수 / 4` 기준의 대략값이다. 모델마다 실제 토큰 수는 달라질 수 있다.

## 설치

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-scanner/master/install.sh | sh
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/youngwoocho02/unity-scanner/master/install.ps1 | iex
```

설치 스크립트는 최신 릴리스 바이너리를 받고 설치 디렉터리를 `PATH`에 추가한다. 설치 후 명령은 `unity-scanner ...`로 실행한다.

### 업데이트

```bash
unity-scanner update
unity-scanner update --check
```

업데이트 확인은 `unity-scanner update` 또는 `unity-scanner update --check`를 실행할 때만 수행한다.

## 명령

```bash
unity-scanner list   -p <project> [path]
unity-scanner read   -p <project> <asset>
unity-scanner search -p <project> [path] [filters]
unity-scanner refs   -p <project> <asset-or-guid> [scan-path]
unity-scanner update [--check]
unity-scanner help [command]
unity-scanner version
```

루트 옵션:

```text
-h, --help             도움말 출력
-v, --version          버전 출력
```

프로젝트 명령 옵션:

```text
-p, --project <path>   Unity 프로젝트 경로
--line-width <n>       출력 한 줄 최대 폭, 기본 1200, 0이면 자르지 않음
--profile              명령 단계별 시간 프로파일 출력
--workers <n>          병렬 worker 수, 기본 CPU 수
```

명령 별칭: `ls` = `list`, `cat` = `read`, `find` = `search`

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
unity-scanner read -p /projects/SampleProject Assets/Examples/Prefabs/SamplePrefab.prefab --component MeshRenderer --path SampleMeshRoot --field-limit 3 --limit 3
```

출력 형태:

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

차이:

```text
일반 grep/find:        약 40줄, 2600글자, 약 650토큰
unity-scanner search: 약 11줄,  320글자, 약 80토큰
감소:                  글자 기준 약 80%
```

경로와 확장자 반복은 그룹으로 줄인다. 파일명이 이름 검색에 이미 맞으면 YAML 내부는 펼치지 않고 `file-name`만 표시한다. 매칭 이유는 `file-name`, `object`, `components`, GUID 참조처럼 구조적으로 표시한다.

넓은 범위 검색은 효과가 있을 때 파일 단위 병렬 처리를 사용한다.

```text
name search:      약 1500ms -> 600ms
guid search:      약 1600ms -> 1000ms
component search: 약 2000ms -> 1100ms
```

## refs

특정 에셋 또는 원본 GUID가 어디서 참조되는지 찾는다.

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

`refs`는 에셋 경로나 32자 원본 GUID를 받는다.

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
--depth <n>       디렉터리 요약 깊이, 기본 무제한
--kind <list>     쉼표로 구분한 종류 목록: prefab,scene,asset,cs,mat
--meta            본문에 .meta 파일 포함
--flat            디렉터리 요약 생략
--limit <n>       최대 그룹 수, 기본 무제한
```

### read

```text
--depth <n>          계층 깊이, 기본 무제한
--path <name/path>   일치하는 오브젝트 브랜치만 표시
--component <name>   일치하는 컴포넌트의 필드 표시
--id <fileID>        로컬 YAML object/component fileID 집중 조회
--field-limit <n>    컴포넌트별 최대 필드 수, 기본 무제한
--limit <n>          최대 GameObject/컴포넌트 매치 수, 기본 무제한
--full-tree          렌더 전용 접기 없이 보이는 트리 행 전부 표시
--override <text>    지정 텍스트와 맞는 prefab override만 표시
--override-limit <n> 최대 prefab override 표시 수, 기본 40, 0은 무제한
--raw-overrides      prefab override 원문 target 참조 표시
--no-resolve         script, GUID, source prefab 경로 해석 생략
```

`read`가 `PREFAB_SOURCES`를 표시하면 로컬 직렬화 YAML 기준이다. source/nested prefab 내용은 펼치지 않으므로 source prefab을 같이 읽거나 Unity `LoadPrefabContents`로 Editor-resolved 상태를 확정한다.

### search

```text
--name <text>        파일명 또는 GameObject 이름 검색
--component <text>   컴포넌트/스크립트 이름 검색
--script-path <path> 지정 에셋 경로 아래 MonoBehaviour 스크립트 검색
--source <text>      prefab source 경로/이름 검색
--guid <guid>        원본 Unity GUID 참조 검색
--ref <guid>         --guid 별칭
--type <list>        prefab,scene,asset,cs,mat
--compact            한 줄 그룹 결과 출력
--warnings <mode>    경고 출력 방식: summary 또는 detail, 기본 summary
--limit <n>          최대 결과 파일 수, 기본 무제한
--object-limit <n>   결과 파일별 최대 오브젝트 표시 수, 기본 12
```

### refs

```text
--type <list>        prefab,scene,asset,mat,controller
--detail             압축 그룹 대신 상세 매치 출력
--warnings <mode>    경고 출력 방식: summary 또는 detail, 기본 summary
--limit <n>          최대 결과 파일 수, 기본 무제한
```

### update

```text
--check              설치하지 않고 업데이트만 확인
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

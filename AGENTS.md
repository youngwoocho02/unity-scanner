# unity-scanner 작업 규칙

## 릴리스 절차

- 변경 작업 전 `git status --short --branch`로 작업트리와 원격 추적 상태를 확인한다.
- 변경 후 `go test ./...`를 통과시킨다.
- 커밋 전 `git status --short`로 의도한 파일만 변경됐는지 확인한다.
- 커밋 메시지는 한글로 목적, 변경 영역, 동작 차이, 검증 결과를 적는다.
- 커밋 후 `git push origin master`로 푸시한다.
- 푸시 후 `git status --short --branch`로 `master`와 `origin/master`가 같은지 확인한다.

## Unity Editor 패키지 버전

- `unity-scanner-sync`는 CLI와 별도 버전으로 관리한다.
- 패키지 버전은 `unity-scanner-sync/package.json`의 `version`이 기준이다.
- 패키지 설치 문서는 `https://github.com/youngwoocho02/unity-scanner.git?path=/unity-scanner-sync#sync/v<version>` 형식을 쓴다.
- 패키지 전용 릴리스 태그는 `sync/v<version>` 형식으로 만든다.
- 패키지만 변경한 경우 CLI 릴리스 태그 `v*`를 새로 만들지 않는다.
- 패키지 변경 커밋은 패키지 파일, README 설치 문서, 이 규칙 문서를 함께 정리한다.
- 다른 Unity 프로젝트의 로컬 `file:` 테스트 manifest 변경은 unity-scanner 커밋에 포함하지 않는다.

## CLI 릴리스 절차

- 기존 태그는 이동하지 않는다. 새 릴리스는 semver 기준 최신 태그의 patch 버전을 올린 새 태그로 만든다.
- 새 태그 예: semver 최신 태그가 `v0.2.3`이면 다음 태그는 `v0.2.4`다.
- 태그는 최신 푸시 커밋에 생성하고 `git push origin refs/tags/<tag>`로 푸시한다.
- 태그 푸시 후 `gh run list --branch <tag> --workflow Release`로 해당 태그의 run-id를 찾고 `gh run watch <run-id> --exit-status`로 Release CI 완료를 확인한다.
- CI 성공 후 `gh release view <tag>`로 릴리스 생성 여부를 확인한다.
- 로컬 설치본은 릴리스 확인 후 `unity-scanner update`로 갱신하고 `unity-scanner version`으로 버전을 확인한다.

## 금지

- 기존 릴리스 태그를 force push로 이동하지 않는다.
- CI 결과를 확인하지 않은 상태로 릴리스 완료를 보고하지 않는다.
- 로컬 업데이트 확인 없이 사용자에게 설치본 갱신 완료를 보고하지 않는다.

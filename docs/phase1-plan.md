# Phase 1 구현 계획

2026-10-26 작성. 기준은 Claude Docs의 'Phase 1 설계'(rev 46)와 '제품 방향과 Phase 설계'다. 설계와 다르게 해야 할 일은 아래 '물어볼 것'에 적고, 정해지면 원본 설계 문서를 고친다.

## 1. 저장소 구조

```
Asksemble/
├─ agent/                     PC 에이전트 (Go 모듈, 단일 실행 파일)
│  ├─ cmd/asksemble-agent/    main: serve · sessions · info · version (뒤에 pair · devices · revoke · uninstall)
│  ├─ internal/core/          Claude를 모르는 공통 타입: Session, State, Capability, Adapter 인터페이스
│  ├─ internal/config/        설정 파일(허용 폴더, 포트, 기본 모델·노력 수준, 폴더별 열어 두기)
│  ├─ internal/logx/          로그. 토큰·대화 내용 키는 지워서 남긴다
│  ├─ internal/devices/       기기 토큰 해시 저장소 (짝 맺기 단계에서 QR·pair를 더함)
│  ├─ internal/api/           HTTP 서버: 127.0.0.1 바인딩, Host 검사, 토큰 확인, /v1 라우트
│  └─ internal/adapter/claude/  Claude 어댑터: claude 실행기, agents --json 파서, 대화 기록 수집기 (Claude 코드는 여기만)
├─ app/                       폰 앱 (React Native + Expo, TypeScript)
│  ├─ src/i18n/               ko.json · en.json
│  ├─ src/connections/        연결 목록 (kind: pc, 뒤에 handoff · direct · cloud-pc)
│  └─ src/api/                에이전트 /v1 클라이언트 (모르는 필드는 무시)
└─ docs/                      이 계획, 뒤에 설치 문서·API 문서. 공개할 때 설계도 여기로 옮긴다
```

## 2. 단계와 관문

설계의 '단계별 계획' 순서를 따른다: Windows 에이전트 → Android → iOS.

| 단계 | 할 일 | 관문 |
| --- | --- | --- |
| A1 | Go 모듈, 설정, 로그, HTTP 서버(`/v1/info`, `/v1/stream`=404), 세션 수집기(읽기만), `GET /v1/sessions` | 단위 테스트 통과. 실제 PC에서 `asksemble-agent sessions`가 1초 안에 목록을 낸다 **(이번에 함)** |
| A2 | 기기 토큰과 짝 맺기: `agent pair`(5분 창, QR, 128비트 코드), `POST /v1/pair`, PC 화면 허용 확인, `devices`·`revoke`, 실패 횟수 제한, `Tailscale-User-Login` 검사 | 폰 브라우저나 curl로 짝 맺고 토큰으로 `/v1/info` 접근 |
| A3 | 실행기: 열기(실행 규칙 5가지), 새 세션, 멈추기, 지우기. 원격 링크 20초 대기, `Workspace not trusted` 잡기, 관리자 서비스 감지 | 가짜 claude 테스트 통과, 실제 PC에서 F3–F6 수동 시험 |
| A4 | 서버 관리자(F10): 폴더별 `claude remote-control`, Ctrl+C 종료, 재시작 시 다시 띄우기, `bridge-pointer.json`으로 '서버' 배지 | 실제 PC에서 Claude 앱으로 그 폴더에 새 세션 생성 |
| A5 | 설치 점검(`check`), serve 포트 고르기 안내, 자동 시작(일반 권한), `uninstall` | **에이전트 관문:** 명령줄 요청만으로 F1–F6, F10 동작 + 확장 원칙 11개 |
| B1 | Expo 앱: 짝 맺기(QR), 세션 목록·검색·필터, 상세 시트, 새 세션, 폴더 화면, 설정, 오류 화면, ko/en | — |
| B2 | Android 내부 테스트 트랙 | **본인 폰에서 1주일 실제 사용** |
| C | iOS(EAS 빌드, TestFlight), 데모 모드 | TestFlight 외부 테스터 심사 통과 |
| D | macOS·Linux 에이전트(구현 전 테스트 5 포함), 서명·공증, 공개 | — |

## 3. 요구사항 → 코드

| ID | 내용 | 지키는 곳 |
| --- | --- | --- |
| F1 | 세션 목록(제목·폴더·시각·상태) | `adapter/claude/transcripts.go`(기록 파일), `agents.go`(실행 상태), `collector.go`(합치기·상태 판정), `api` `GET /v1/sessions` |
| F2 | 검색·필터 | `core.Filter`(q·folder·state), 앱 목록 화면 |
| F3 | 원격으로 열기 | `adapter/claude/runner.go` 실행 규칙(A3), `api` `POST /v1/sessions/{id}/open`, 앱 `Linking.openURL` |
| F4 | 새 세션 | `runner.go` 새 세션 인자(A3), `config.Folders` 허용 목록, 앱 새 세션 화면 |
| F5 | 멈추기·지우기 | `runner.go` `stop`·`rm`(A3), 앱 지우기 재확인 |
| F6 | PC에서 이어 보기 안내 | `core.Session.AttachCommand`(어댑터가 채움), 앱 복사 버튼 |
| F7 | 여러 PC | 앱 `connections`(연결 목록, 원칙 8) |
| F10 | 폴더 열어 두기 | `config.Folder.Server`, `adapter/claude/server.go`(A4), `api` `GET /v1/folders`·`PUT /v1/folders/{id}/server` |
| N1 | 중계 서버 없음 | 에이전트는 외부로 나가는 요청이 없다. 앱은 연결 목록의 주소로만 접속 |
| N2 | 정해진 동작만 | `runner.go`: 명령 종류(enum)별로 인자를 만들고 금지 플래그 검사. 셸 없이 `exec.Command(exe, args...)`. `.cmd`/`.bat`은 실행 거부 |
| N3 | 일반 권한 | 시작할 때 관리자 권한이면 경고(A5), 관리자 서비스 감지(A3) |
| N4 | 짝 맺은 기기만 | `devices` 해시 저장소 + `api` 토큰 미들웨어(A1에서 틀, A2에서 완성) |
| N5 | 빠름 | 수집기 캐시: 파일별 크기·수정 시각, 덧붙은 부분만 읽기 |
| N6 | 플랫폼 | Go 크로스 빌드, 경로는 `os.UserHomeDir`·`os.UserConfigDir` |
| N7 | 설치 쉬움 | 단일 실행 파일, QR 짝 맺기 |
| N8 | 비밀값 보호 | 토큰은 SHA-256 해시만 저장, `logx`가 token·authorization·prompt·content 키를 지움, 앱은 `expo-secure-store` |
| N9 | 다국어 | 앱 `src/i18n/ko.json`·`en.json`. 에이전트 API는 코드(`code`)만 돌려주고 문장은 앱이 고른다 |
| N10 | 로그인 정보 비접촉 | 수집기는 `~/.claude/projects`와 `daemon.lock`·`daemon.status.json`만 읽는다. `.credentials.json`·`~/.claude.json`·`sessions/*.key`는 열지 않는다(테스트로 확인) |

| # | 확장 원칙 | 지키는 곳 |
| --- | --- | --- |
| 1 | `claude:<uuid>`와 `agent` 필드 | `core.SessionID`(형식 검사), 모든 세션 응답에 `agent` |
| 2 | Claude 코드는 어댑터 하나에만 | `internal/adapter/claude`만 Claude를 안다. `core`·`api`·`config`·`devices`는 `core.Adapter` 인터페이스만 쓴다 |
| 3 | capability 목록 | `core.Capability`, `/v1/info`의 `capabilities`(어댑터별)와 `reserved`(ask·tui·chat) |
| 4 | `/v1`은 필드를 더하기만 | API 타입에 필드 삭제 금지 규칙(주석), 앱 파서는 모르는 필드 무시 |
| 5 | `/v1/stream` 자리 | `api`가 404 + `code: not_implemented` |
| 6 | 열기 방식 자리 | 세션 응답 `openModes: ["official"]`, 앱 상세 시트의 열기 방식 영역 |
| 7 | 권한 해제 옵션 금지 | `runner.go`의 금지 플래그 검사(모든 실행에 적용) + 테스트 |
| 8 | 연결 종류 | 앱 `connections` 저장 구조의 `kind` |
| 9 | 두 입구 | 앱 탭 구조에 '한 번에 묻기' 탭 자리(숨김) |
| 10 | 다국어 | N9와 같음 |
| 11 | 로그인 정보 비접촉 | N10과 같음 |

## 4. 테스트 방법

- **단위 테스트** (`go test ./...`): 설정 검증, 로그 가리기, Host 검사, 토큰 확인, 대화 기록 파서(`testdata/`의 가짜 기록), `agents --json` 파서, 상태 판정, 필터. 실제 대화 기록은 저장소에 넣지 않는다.
- **가짜 `claude` 실행기 테스트**: 테스트가 `internal/adapter/claude/testdata/fakeclaude`를 임시 폴더에 빌드하고 그 경로로 실행기를 돌린다. 가짜는 받은 인자를 파일에 적고 정해 둔 출력을 낸다. 인자 배열, 금지 플래그 거부, 시간 초과, 종료 코드, `Workspace not trusted` 처리를 본다.
- **실제 PC 수동 시험**: 일반 권한 셸에서. A1은 읽기 명령(`claude agents --json --all`, `claude --version`)만. A3부터 실제 열기·새 세션은 사용자 확인 뒤 시험 폴더에서. Tailscale serve 설정은 사용자가 정한 포트로 사용자 확인 뒤에.

## 5. A1에서 할 것 (아직 시작 전, 2026-10-26 사용량 한도로 멈춤)

이번 세션에서는 형식 확인과 이 계획까지만 했다. 확인한 형식(메타데이터만 봄): `agents --json --all`은 배열이고 항목에 `pid`·`id`·`cwd`·`kind`·`startedAt`(ms)·`sessionId`·`name`·`status`·`state`가 있다. 기록 폴더에는 `<uuid>.orphaned-<시각>-<해시>.jsonl` 파일과 빈 `.jsonl`이 있어 빼야 한다. `claude`는 `~\.local\bin\claude.exe`(네이티브 실행 파일)다. 가장 큰 기록 파일은 약 38MB, 전체 약 217MB라 캐시가 꼭 필요하다.

- `agent/` Go 모듈(`go 1.27`, 표준 라이브러리만).
- `asksemble-agent serve`: 127.0.0.1에만 열고 Host 검사, 토큰 미들웨어, `GET /v1/info`, `GET /v1/sessions`, `/v1/stream` 404.
- `asksemble-agent sessions [--json]`: 수집기 결과를 PC에서 바로 확인(제목·폴더·시각·상태만).
- 수집기: `custom-title` → `agent-name` → `ai-title` 순 제목, `<uuid>/custom-title.json` 보조, 마지막 `cwd`·`timestamp`·모델, 마지막 `bridge_status` 링크, `.orphaned` 파일과 빈 파일 제외, 덧붙은 부분만 읽는 캐시.

## 6. 물어볼 것

1. **Go 모듈 경로**: 지금은 `github.com/asksemble/asksemble/agent`로 두었다. GitHub 조직 이름이 아직 '정할 것'이라, 정해지면 바꾼다.
2. **실행 파일 이름**: `asksemble-agent`로 두었다(설계 문서에는 `agent pair`처럼 적혀 있다). 괜찮은지.
3. **설정 파일 위치**: Windows는 `%AppData%\Asksemble\agent.json`(= `os.UserConfigDir()`), 기기 토큰 해시는 같은 폴더 `devices.json`. 설계에는 '사용자 폴더'라고만 적혀 있다.
4. **`<uuid>/custom-title.json`**: 기록 폴더에 이 파일만 있는 세션이 있다(데스크톱 앱이 쓴 것으로 보임). 설계의 제목 순서에는 없어서, 기록 안의 `custom-title`이 없을 때 쓰는 보조로 넣었다. 설계 문서에 더할지.
5. **'멈춤' 판정**: 지금 PC에는 멈춘 bg 세션이 없어 `agents --json --all`에서 멈춘 항목의 `status`·`state` 값을 확인하지 못했다. 지금은 `kind: background`이면서 pid가 없거나 살아 있지 않으면 멈춤으로 본다. 사용자 확인 뒤 시험 세션 하나를 멈춰 값을 확인하고 싶다.
6. **기록의 `entrypoint: claude-desktop`**: 데스크톱 앱에서 연 세션도 같은 폴더에 기록된다. 이것도 목록에 보여 주고 `--resume`으로 열어도 되는지(데스크톱 앱이 같은 세션을 열고 있으면 기록이 꼬일 수 있음). 지금은 목록에는 넣고 배지(`desktop`)를 단다.
7. **git 시작 시점**: 킥오프 지시대로 git은 아직 시작하지 않았다. PC에서 이 폴더를 한 번 신뢰한 뒤 `git init`할지 알려 주면 그때 한다.

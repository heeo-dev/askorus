# Phase 1 구현 계획

2026-10-26 작성. 기준은 Claude Docs의 'Phase 1 설계'(rev 46, 7절의 결정을 반영한 것은 rev 49)와 '제품 방향과 Phase 설계'다. 설계와 다르게 해야 할 일은 아래 '물어볼 것'에 적고, 정해지면 원본 설계 문서를 고친다.

## 1. 저장소 구조

```
Asksemble/
├─ agent/                     PC 에이전트 (Go 모듈, 단일 실행 파일)
│  ├─ cmd/asksemble-agent/    main: serve · pair · sessions · info · devices · version (뒤에 check · uninstall)
│  ├─ internal/core/          Claude를 모르는 공통 타입: Session, State, Capability, Adapter 인터페이스
│  ├─ internal/config/        설정 파일(허용 폴더, 포트, 기본 모델·노력 수준, 폴더별 열어 두기)
│  ├─ internal/logx/          로그. 토큰·대화 내용 키는 지워서 남긴다
│  ├─ internal/devices/       기기 토큰 해시 저장소 (파일이 바뀌면 다시 읽어 서버와 CLI가 같이 씀)
│  ├─ internal/pairing/       짝 맺기 창·코드·PC 허용 (pair 명령과 서버가 파일로 주고받음)
│  ├─ internal/tailnet/       tailscale CLI 읽기 전용: 주인 계정, MagicDNS 이름, serve·funnel 상태
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
| A1 | Go 모듈, 설정, 로그, HTTP 서버(`/v1/info`, `/v1/stream`=404), 세션 수집기(읽기만), `GET /v1/sessions` | 단위 테스트 통과. 실제 PC에서 `asksemble-agent sessions`가 1초 안에 목록을 낸다 **(완료)** |
| A2 | 기기 토큰과 짝 맺기: `asksemble-agent pair`(5분 창, QR, 128비트 코드), `POST /v1/pair`, PC 화면 허용 확인, `devices`·`revoke`, 실패 횟수 제한, `Tailscale-User-Login` 검사 | 폰 브라우저나 curl로 짝 맺고 토큰으로 `/v1/info` 접근 **(PC 안 시험까지 완료. 폰에서 하는 시험은 serve 설정 뒤)** |
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
| F1 | 세션 목록(제목·폴더·시각·상태) | `adapter/claude/transcripts.go`(기록 파일), `agents.go`(실행 상태), `adapter.go`(합치기·상태 판정·캐시), `api` `GET /v1/sessions` |
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
| N4 | 짝 맺은 기기만 | `devices` 해시 저장소, `api` 토큰 미들웨어와 실패 횟수 제한(`api/pair.go`), `pairing`(QR 코드·PC 허용), `devices revoke` |
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
- **가짜 `claude` 실행기 테스트**: 테스트 실행 파일이 `ASKSEMBLE_FAKE_CLAUDE=1`이면 가짜 `claude`로 동작한다(`internal/adapter/claude/fake_test.go`). 가짜는 받은 인자를 파일에 적고 정해 둔 출력·종료 코드를 낸다. 인자 배열, 금지 플래그 거부(거부되면 프로세스를 아예 띄우지 않음), 시간 초과, 종료 코드를 보고, A3에서 `Workspace not trusted` 처리를 더한다.
- **실제 PC 수동 시험**: 일반 권한 셸에서. A1은 읽기 명령(`claude agents --json --all`, `claude --version`)만. A3부터 실제 열기·새 세션은 사용자 확인 뒤 시험 폴더에서. Tailscale serve 설정은 사용자가 정한 포트로 사용자 확인 뒤에.

## 5. A1 결과 (2026-10-26)

확인한 형식(메타데이터만 봄): `agents --json --all`은 배열이고 항목에 `pid`·`id`·`cwd`·`kind`·`startedAt`(ms)·`sessionId`·`name`·`status`·`state`가 있다. 기록 폴더에는 `<uuid>.orphaned-<시각>-<해시>.jsonl` 파일과 빈 `.jsonl`이 있어 뺀다. `claude`는 `~\.local\bin\claude.exe`(네이티브 실행 파일)다. 가장 큰 기록 파일은 약 38MB, 전체 약 217MB다.

만든 것:

- `agent/` Go 모듈(`go 1.27.3`, 표준 라이브러리만).
- `asksemble-agent serve`: 127.0.0.1에만 열고 Host 검사(127.0.0.1·localhost·::1·설정의 `serveHosts`), `Origin` 헤더·OPTIONS 거부(CORS 없음), 기기 토큰 확인, `Tailscale-User-Login`이 `tailscaleOwner`와 다르면 거부, `GET /v1/info`, `GET /v1/sessions?q=&folder=&state=&agent=`, `/v1/stream`은 404 `not_implemented`. 로그에는 시각·경로(쿼리 제외)·기기 id·상태 코드·걸린 시간만.
- `asksemble-agent sessions [-state] [-q] [-folder] [-json]`, `info`, `devices list|add|revoke`(짝 맺기 전 임시 도구. A2에서 QR 짝 맺기로 바꾼다).
- 수집기: `custom-title` → `<uuid>/custom-title.json` → `agent-name` → `ai-title` 순 제목, 사이드체인 줄 제외, 마지막 `cwd`·`timestamp`·모델, 마지막 `bridge_status` 링크(`https://claude.ai/code/session_…`만 받음), 마지막 질문 앞 80자, 덧붙은 부분만 읽는 캐시.
- 상태: `background`+살아 있는 pid → running, `background` 그 밖 → stopped, 그 밖의 kind+pid → terminal, 기록만 → history. `agents`를 못 읽으면 전부 unknown(잘못 '기록만'으로 보고 이미 열린 세션을 재개하지 않게).
- 실행기: 셸 없이 인자 배열, 허용 목록(지금은 `--version`, `agents --json --all`만) + 권한 플래그 거부 목록, `.cmd`·`.bat` 등 스크립트 거부, 15초 시간 제한, Windows에서 콘솔 창 숨김.

시험 결과:

- `go vet ./...`, `go test ./...` 모두 통과(7개 패키지).
- 실제 PC(읽기 명령만): 세션 70개(running 1, terminal 1, history 68), 원격 링크 있는 것 34개, desktop 배지 16개.
- 속도: `claude agents --json --all` 하나가 1.3~1.5초 걸린다. 그래서 기록 읽기와 동시에 돌리고, 결과는 2초 동안 그대로 쓰고 1분까지는 먼저 돌려준 뒤 뒤에서 새로 받는다. `serve` 시작 때 캐시를 채운다. 첫 실행(캐시 없음)은 약 2초, 그 뒤 `GET /v1/sessions`는 20ms 안팎(20번 잰 값 19–72ms). N5(1초) 충족은 서버가 켜져 있는 동안 기준이다.
- 임시 설정 폴더와 임시 토큰으로 `serve`를 띄워 확인: 토큰 없으면 401, `/v1/stream` 404, 로그에 토큰·검색어 없음. 시험 뒤 서버를 끄고 토큰 파일을 지웠다.

남은 것(A1 범위 밖으로 미룸): 관리자 권한 실행 경고(A5), `app/` Expo 뼈대.

## 6. A2 결과 (2026-10-26)

만든 것:

- `asksemble-agent pair`: 서버가 켜져 있는지 보고, `tailscale serve status`에서 에이전트 포트를 가리키는 HTTPS 주소를 찾아 5분짜리 짝 맺기 창을 연다. 터미널에 QR(어두운 배경 기준, `-light-terminal`로 바꿈)과 손으로 넣을 코드, 같은 QR의 PNG 경로를 보여 준다. serve가 아직 없으면 설정 명령을 안내만 하고 직접 바꾸지 않는다. `-host`·`-port`로 직접 줄 수도 있다.
- QR 내용: `asksemble://pair?v=1&host=<MagicDNS 이름>&port=<serve HTTPS 포트>&code=<코드>`. 코드는 128비트 난수를 base32로 쓴 26자(A–Z, 2–7). 앱의 딥 링크로도 쓴다.
- `POST /v1/pair {code, deviceName}`(토큰 없이 받는 유일한 경로): `Tailscale-User-Login` 헤더가 있고 `X-Forwarded-For` 마지막 값이 Tailscale 주소(100.64.0.0/10, fd7a:115c:a1e0::/48)여야 하고, 로그인이 PC 주인 계정과 같아야 한다. 맞으면 PC의 `pair` 창에 "허용할까요? [y/N]"를 띄우고 최대 2분 기다린다. 허용하면 256비트 토큰을 발급해 한 번만 돌려준다.
- 한 번만 쓰는 코드: 맞는 코드가 처음 들어오면 허용·거절·시간 초과와 상관없이 창이 닫힌다. 틀린 코드 5번이면 창이 닫힌다. 다른 기기가 짝 맺는 중이면 409.
- PC 주인 계정: 설정의 `tailscaleOwner`가 없으면 `serve` 시작 때 `tailscale status --json`에서 읽는다. 모르면 짝 맺기는 503 `owner_unknown`. MagicDNS 이름은 Host 허용 목록에 자동으로 더한다.
- Funnel: `serve` 시작 때 에이전트 포트에 Funnel이 켜져 있으면 시작을 거부한다(설계 '보안').
- 실패 횟수 제한: 주소마다 10분에 10번 틀리면 10분 동안 429. 모든 주소를 합쳐 10분에 100번이면 전부 10분 동안 429(주소를 바꿔 가며 시도하는 로컬 프로그램 대비). 토큰과 짝 맺기 코드 모두 센다.
- 기기 저장소는 파일이 바뀌면 다시 읽는다. 서버가 켜진 채 다른 창에서 `devices revoke`를 하면 그 토큰은 바로 401이 된다. 파일을 못 읽으면 어떤 토큰도 받지 않는다.
- `devices add`(A1의 임시 도구)는 없앴다. 기기는 `pair`로만 더한다.

시험 결과:

- `go vet ./...`, `go test ./...` 모두 통과(9개 패키지). 짝 맺기 허용·거절·시간 초과·틀린 코드 잠금·만료·동시 요청·이름 검사, 헤더·주소·계정 검사, 실패 횟수 제한, 다른 프로세스의 revoke 반영을 테스트한다.
- 실제 PC(임시 설정 폴더, serve 설정은 그대로): `serve` 시작 때 Tailscale 상태를 읽었고 "serve가 아직 에이전트를 가리키지 않음"을 알렸다. `pair`로 QR을 띄우고 serve가 붙이는 헤더를 흉내 낸 로컬 요청으로 짝 맺기를 끝까지 했다. 다른 계정 403, 틀린 코드 403, LAN 주소 403, 맞는 코드 + PC에서 y → 200과 토큰, 그 토큰으로 `/v1/sessions` 200, 같은 코드 다시 쓰기 404, 서버가 켜진 채 `devices revoke` → 바로 401. `devices.json`과 로그에 토큰이 없고, 끝나면 짝 맺기 파일이 남지 않았다. 시험 뒤 서버를 끄고 임시 폴더를 지웠다.
- 처음 시험에서 PowerShell 파이프로 넣은 "y"가 거절로 읽혔다. 답 앞의 BOM을 지우고, y가 아닌 답은 무엇을 받았는지 보여 주게 고친 뒤 다시 시험해 통과했다.

남은 것: 실제 폰에서 Tailscale을 거쳐 하는 시험(serve 설정 필요), PC 트레이 메뉴의 허용 창(A5), 시작 뒤 Funnel이 켜지는 경우의 주기 점검(A5).

## 7. 정한 것 (2026-10-26, 사용자가 '제안대로'로 정함)

1. Go 모듈 경로는 `github.com/asksemble/asksemble/agent`. GitHub 조직 이름이 바뀌면 같이 바꾼다.
2. 실행 파일 이름은 `asksemble-agent`. 설계 문서의 `agent pair`·`agent devices`·`agent revoke`는 `asksemble-agent pair`·`asksemble-agent devices`·`asksemble-agent devices revoke`다.
3. 설정 위치는 `os.UserConfigDir()/Asksemble`(Windows `%AppData%\Asksemble`). `agent.json`(설정), `devices.json`(토큰 해시), `logs\agent.log`. `ASKSEMBLE_AGENT_DIR`로 바꿀 수 있다.
4. 제목 순서는 `custom-title` → `<uuid>/custom-title.json` → `agent-name` → `ai-title`.
5. 데스크톱 앱 세션(`entrypoint: claude-desktop`)도 목록에 넣고 `desktop` 배지를 단다.
6. 목록의 `agents` 결과는 2초 동안 그대로, 1분까지는 먼저 돌려주고 뒤에서 새로 받는다. 에이전트가 상태를 바꾸면 바로 지운다.
7. `agents`를 못 읽으면 상태는 `unknown`이고, 이어 보기 명령을 주지 않는다.
8. 위 2–7과 A2의 짝 맺기 세부는 원본 'Phase 1 설계'에도 적었다.

## 8. 물어볼 것

1. **Tailscale serve 설정**: 폰에서 시험하려면 `tailscale serve --bg --https=<포트> http://127.0.0.1:8790`이 필요하다. 이 PC는 443·8443·8767을 쓰고 있어서 `8790`을 제안한다. 다른 serve 설정은 건드리지 않는다. 해도 되는지.
2. **'멈춤' 판정 확인**: 멈춘 bg 세션이 `agents --json --all`에 어떻게 나오는지 아직 못 봤다. 시험 폴더에 bg 세션을 하나 띄웠다가 `claude stop`으로 멈춰 값을 보고 지우고 싶다. 해도 되는지(세션을 띄우고 멈추는 일이라 따로 묻는다).
3. **폰에서 짝 맺기 시험 방법**: 앱(B1)이 생기기 전에는 폰 브라우저로 POST를 보내기 어렵다. A3를 먼저 하고 B1에서 실제 폰으로 시험할지, 아니면 그 전에 시험용 간단한 웹 페이지를 에이전트에 잠깐 붙일지.
4. **PC 쪽 문구**: `pair`·`serve`의 터미널 문구는 지금 영어만 있다. 앱 화면(N9)과 달리 PC 명령줄 문구도 한국어를 같이 낼지.

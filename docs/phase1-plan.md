# Phase 1 구현 계획

2026-10-26 작성. 기준은 Claude Docs의 'Phase 1 설계'(rev 46, 7절의 결정과 짝 맺기 다시 묻기를 반영한 것은 rev 51, A3 결정과 작업 방식은 rev 54, N5 목표는 rev 55, A4 서버 모드는 rev 56, 세션 파일은 rev 57)와 '제품 방향과 Phase 설계'다. 설계와 다르게 해야 할 일은 아래 '물어볼 것'에 적고, 정해지면 원본 설계 문서를 고친다.

## 1. 저장소 구조

```
Askorus/
├─ agent/                     PC 에이전트 (Go 모듈, 단일 실행 파일)
│  ├─ cmd/askorus-agent/    main: serve · pair · sessions · info · devices · version (뒤에 check · uninstall)
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
| A1 | Go 모듈, 설정, 로그, HTTP 서버(`/v1/info`, `/v1/stream`=404), 세션 수집기(읽기만), `GET /v1/sessions` | 단위 테스트 통과. 실제 PC에서 `askorus-agent sessions`가 1초 안에 목록을 낸다 **(완료)** |
| A2 | 기기 토큰과 짝 맺기: `askorus-agent pair`(5분 창, QR, 128비트 코드), `POST /v1/pair`, PC 화면 허용 확인, `devices`·`revoke`, 실패 횟수 제한, `Tailscale-User-Login` 검사 | 폰 브라우저나 curl로 짝 맺고 토큰으로 `/v1/info` 접근 **(PC 안 시험까지 완료. 폰에서 하는 시험은 serve 설정 뒤)** |
| A3 | 실행기: 열기(실행 규칙 5가지), 새 세션, 멈추기, 지우기. 원격 링크 20초 대기, `Workspace not trusted` 잡기. 열기·새 세션은 작업 id 방식 | 가짜 claude 테스트 통과, 실제 PC에서 F3–F6 수동 시험 **(완료, 8절)** |
| A4 | 서버 관리자(F10): 폴더별 `claude remote-control`, Ctrl+C 종료, 재시작 시 다시 띄우기, 서버 세션에 '서버' 배지 | 실제 PC에서 Claude 앱으로 그 폴더에 새 세션 생성 **(PC 쪽 시험 완료, 폰에서 새 세션 만들기는 사용자 확인 대기, 10절)** |
| A5 | 설치 점검(`check`), serve 포트 고르기 안내, 자동 시작(일반 권한), `uninstall`, 관리자 권한 서비스 감지(A3에서 옮김, 2026-10-26) | **에이전트 관문:** 명령줄 요청만으로 F1–F6, F10 동작 + 확장 원칙 11개 **(만듦. 자동 시작 등록은 사용자 확인 대기, 11절)** |
| B1 | Expo 앱: 짝 맺기(QR), 세션 목록·검색·필터, 상세 시트, 새 세션, 폴더 화면, 설정, 오류 화면, ko/en (13절) | 실제 폰에서 시험 페이지로 되던 것이 앱으로 모두 됨 |
| B2 | Android 내부 테스트 트랙(15절, 절차는 `docs/android-release.md`) | **본인 폰에서 1주일 실제 사용** **(B2-1 완료, B2-2는 MT-05·08만 남음. 나머지는 미룸, 18절)** |
| C | iOS(EAS 빌드, TestFlight), 데모 모드 | TestFlight 외부 테스터 심사 통과 **(미룸, 18절)** |
| D | macOS·Linux 에이전트(구현 전 테스트 5 포함), 서명·공증, 공개 | — **(미룸, 18절)** |
| E | 2차: 최근 대화 미리보기(F8), 우리 쪽 알림(F9), Tailscale 내장(`tsnet`) | 다음 (18절) |
| P2 | Phase 2 한 번에 묻기(제품 방향 문서) | E 다음 |

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
| N1 | 중계 서버 없음 | 에이전트는 기본으로 외부로 나가는 요청이 없다. 예외는 사용자가 켠 연동(알림 푸시, 판정 모델)뿐이고 무엇이 어디로 가는지 앱 설정과 `check`에 보인다(2026-10-29 다듬음, 설계 rev 67). AI CLI가 스스로 하는 통신은 규칙 밖. 앱은 연결 목록의 주소로만 접속 |
| N2 | 정해진 동작만 | `runner.go`: 명령 종류(enum)별로 인자를 만들고 금지 플래그 검사. 셸 없이 `exec.Command(exe, args...)`. `.cmd`/`.bat`은 실행 거부 |
| N3 | 일반 권한 | `serve`는 관리자 권한이면 시작을 거부, 자동 시작은 HKCU Run(일반 권한), 관리자 서비스 감지(`adapter/claude/daemon.go`) |
| N4 | 짝 맺은 기기만 | `devices` 해시 저장소, `api` 토큰 미들웨어와 실패 횟수 제한(`api/pair.go`), `pairing`(QR 코드·PC 허용), `devices revoke` |
| N5 | 빠름 | 수집기 캐시: 파일별 크기·수정 시각, 덧붙은 부분만 읽기 |
| N6 | 플랫폼 | Go 크로스 빌드, 경로는 `os.UserHomeDir`·`os.UserConfigDir` |
| N7 | 설치 쉬움 | 단일 실행 파일, QR 짝 맺기 |
| N8 | 비밀값 보호 | 토큰은 SHA-256 해시만 저장, `logx`가 token·authorization·prompt·content 키를 지움, 앱은 `expo-secure-store` |
| N9 | 다국어 | 앱 `src/i18n/ko.json`·`en.json`. 에이전트 API는 코드(`code`)만 돌려주고 문장은 앱이 고른다 |
| N10 | 로그인 정보 비접촉 | 수집기는 `~/.claude/projects`, `~/.claude/sessions/<pid>.json`(이름이 숫자.json인 파일만, 2026-10-26 사용자 허락), `daemon.lock`·`daemon.status.json`만 읽는다. `.credentials.json`·`~/.claude.json`·`sessions/*.key`는 열지 않는다(테스트로 확인) |

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

2026-10-27부터 테스트 기준은 `docs/testing.md`다(단계, 회귀 규칙, 시나리오 목록, 회귀 표). 아래는 처음 계획이다.

- **단위 테스트** (`go test ./...`): 설정 검증, 로그 가리기, Host 검사, 토큰 확인, 대화 기록 파서(`testdata/`의 가짜 기록), `agents --json` 파서, 상태 판정, 필터. 실제 대화 기록은 저장소에 넣지 않는다.
- **가짜 `claude` 실행기 테스트**: 테스트 실행 파일이 `ASKORUS_FAKE_CLAUDE=1`이면 가짜 `claude`로 동작한다(`internal/adapter/claude/fake_test.go`). 가짜는 받은 인자를 파일에 적고 정해 둔 출력·종료 코드를 낸다. 인자 배열, 금지 플래그 거부(거부되면 프로세스를 아예 띄우지 않음), 시간 초과, 종료 코드를 보고, A3에서 `Workspace not trusted` 처리를 더한다.
- **실제 PC 수동 시험**: 일반 권한 셸에서. A1은 읽기 명령(`claude agents --json --all`, `claude --version`)만. A3부터 실제 열기·새 세션은 사용자 확인 뒤 시험 폴더에서. Tailscale serve 설정은 사용자가 정한 포트로 사용자 확인 뒤에.

## 5. A1 결과 (2026-10-26)

확인한 형식(메타데이터만 봄): `agents --json --all`은 배열이고 항목에 `pid`·`id`·`cwd`·`kind`·`startedAt`(ms)·`sessionId`·`name`·`status`·`state`가 있다. 기록 폴더에는 `<uuid>.orphaned-<시각>-<해시>.jsonl` 파일과 빈 `.jsonl`이 있어 뺀다. `claude`는 `~\.local\bin\claude.exe`(네이티브 실행 파일)다. 가장 큰 기록 파일은 약 38MB, 전체 약 217MB다.

만든 것:

- `agent/` Go 모듈(`go 1.27.3`, 표준 라이브러리만).
- `askorus-agent serve`: 127.0.0.1에만 열고 Host 검사(127.0.0.1·localhost·::1·설정의 `serveHosts`), `Origin` 헤더·OPTIONS 거부(CORS 없음), 기기 토큰 확인, `Tailscale-User-Login`이 `tailscaleOwner`와 다르면 거부, `GET /v1/info`, `GET /v1/sessions?q=&folder=&state=&agent=`, `/v1/stream`은 404 `not_implemented`. 로그에는 시각·경로(쿼리 제외)·기기 id·상태 코드·걸린 시간만.
- `askorus-agent sessions [-state] [-q] [-folder] [-json]`, `info`, `devices list|add|revoke`(짝 맺기 전 임시 도구. A2에서 QR 짝 맺기로 바꾼다).
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

- `askorus-agent pair`: 서버가 켜져 있는지 보고, `tailscale serve status`에서 에이전트 포트를 가리키는 HTTPS 주소를 찾아 5분짜리 짝 맺기 창을 연다. 터미널에 QR(어두운 배경 기준, `-light-terminal`로 바꿈)과 손으로 넣을 코드, 같은 QR의 PNG 경로를 보여 준다. serve가 아직 없으면 설정 명령을 안내만 하고 직접 바꾸지 않는다. `-host`·`-port`로 직접 줄 수도 있다.
- QR 내용: `askorus://pair?v=1&host=<MagicDNS 이름>&port=<serve HTTPS 포트>&code=<코드>`. 코드는 128비트 난수를 base32로 쓴 26자(A–Z, 2–7). 앱의 딥 링크로도 쓴다. 앱은 host가 `ts.net` 아래 이름일 때만 받는다(R-94, 11-10: 남이 보낸 링크가 다른 서버를 가리키면 그 서버가 스스로 허용하고 질문을 받게 된다). 확인 화면에 '내 PC 화면에 나온 주소와 같을 때만, 남이 보낸 링크나 QR이면 짝 맺지 마세요'를 보이고, 이미 다른 PC가 있으면 새 PC를 현재 PC로 바꾸지 않고 PC 고르기를 연다(목록에 주소도 보임). PC가 보낸 원격 링크는 앱이 `https://claude.ai/code/session_…` 꼴일 때만 연다.
- `POST /v1/pair {code, deviceName}`(토큰 없이 받는 유일한 경로): `Tailscale-User-Login` 헤더가 있고 `X-Forwarded-For` 마지막 값이 Tailscale 주소(100.64.0.0/10, fd7a:115c:a1e0::/48)여야 하고, 로그인이 PC 주인 계정과 같아야 한다. 맞으면 바로 202와 요청 id를 돌려주고, PC의 `pair` 창에 "허용할까요? [y/N]"를 띄운다(PC 답은 최대 2분). 폰은 `POST /v1/pair/status {requestId}`를 1초마다 보내 결과를 묻는다. 허용되면 첫 응답에 256비트 토큰을 한 번만 담아 준다. 요청 id는 로그에 남지 않게 본문으로 보낸다. 허용 뒤 5분 안에 토큰을 가져가지 않으면 그 기기를 다시 지운다.
- 처음에는 폰이 요청 하나를 열어 둔 채 PC 답을 기다렸다. 실제 폰 시험(2026-10-26)에서 브라우저를 벗어나자 그 요청이 18초 만에 끊겨(408) 지금 방식으로 바꿨다. 시험 페이지는 요청 id를 탭에 저장해 새로 고쳐도 이어서 묻는다.
- 한 번만 쓰는 코드: 맞는 코드가 처음 들어오면 허용·거절·시간 초과와 상관없이 창이 닫힌다. 틀린 코드 5번이면 창이 닫힌다. 다른 기기가 짝 맺는 중이면 409.
- PC 주인 계정: 설정의 `tailscaleOwner`가 없으면 `serve` 시작 때 `tailscale status --json`에서 읽는다. 모르면 짝 맺기는 503 `owner_unknown`. MagicDNS 이름은 Host 허용 목록에 자동으로 더한다.
- Funnel: `serve` 시작 때 에이전트 포트에 Funnel이 켜져 있으면 시작을 거부한다(설계 '보안').
- 실패 횟수 제한: 주소마다 10분에 10번 틀리면 10분 동안 429. 모든 주소를 합쳐 10분에 100번이면 전부 10분 동안 429(주소를 바꿔 가며 시도하는 로컬 프로그램 대비). 토큰과 짝 맺기 코드 모두 센다.
- 기기 저장소는 파일이 바뀌면 다시 읽는다. 서버가 켜진 채 다른 창에서 `devices revoke`를 하면 그 토큰은 바로 401이 된다. 파일을 못 읽으면 어떤 토큰도 받지 않는다.
- `devices add`(A1의 임시 도구)는 없앴다. 기기는 `pair`로만 더한다.

시험 결과:

- `go vet ./...`, `go test ./...` 모두 통과(9개 패키지). 짝 맺기 허용·거절·시간 초과·틀린 코드 잠금·만료·동시 요청·이름 검사, 헤더·주소·계정 검사, 실패 횟수 제한, 다른 프로세스의 revoke 반영을 테스트한다.
- 실제 PC(임시 설정 폴더, serve 설정은 그대로): `serve` 시작 때 Tailscale 상태를 읽었고 "serve가 아직 에이전트를 가리키지 않음"을 알렸다. `pair`로 QR을 띄우고 serve가 붙이는 헤더를 흉내 낸 로컬 요청으로 짝 맺기를 끝까지 했다. 다른 계정 403, 틀린 코드 403, LAN 주소 403, 맞는 코드 + PC에서 y → 200과 토큰, 그 토큰으로 `/v1/sessions` 200, 같은 코드 다시 쓰기 404, 서버가 켜진 채 `devices revoke` → 바로 401. `devices.json`과 로그에 토큰이 없고, 끝나면 짝 맺기 파일이 남지 않았다. 시험 뒤 서버를 끄고 임시 폴더를 지웠다.
- 새 방식(시작 뒤 다시 묻기)도 PC 안에서 끝까지 시험했다: 시작 202(154ms), 5초 자리를 비운 뒤 다시 물어 approved와 토큰, 같은 id로 다시 물으면 404, 그 토큰으로 `/v1/info` 200, 로그에 요청 id·토큰 없음.
- 처음 시험에서 PowerShell 파이프로 넣은 "y"가 거절로 읽혔다. 답 앞의 BOM을 지우고, y가 아닌 답은 무엇을 받았는지 보여 주게 고친 뒤 다시 시험해 통과했다.

실제 폰 시험(2026-10-26, S23, serve 8790): 시험 페이지로 짝 맺기와 세션 목록이 됐다. 알게 된 것 두 가지를 B1에 넘긴다.

- 삼성 인터넷은 `https://claude.ai/code/session_…` 링크를 Claude 앱으로 넘기지 않고 새 탭에서 연다(주소창 옆 앱 아이콘을 눌러야 앱으로 감). 시험 페이지는 Android에서 `intent://…#Intent;scheme=https;package=com.anthropic.claude;S.browser_fallback_url=…;end`로 앱을 지정해 연다(삼성 인터넷에서 바로 앱으로 열리는 것을 확인). 앱에서도 Android는 패키지를 지정해 열고, 앱이 없으면 https로 연다.
- 앱으로 넘어갔다 돌아오면 브라우저가 새 탭으로 열 수 있다. 시험 페이지는 토큰을 탭이 아니라 브라우저(localStorage)에 둔다. 앱은 보안 저장소에 두므로 상관없다.

남은 것: 앱(B1)에서 실제 폰으로 다시 하는 시험, PC 트레이 메뉴의 허용 창(A5), 시작 뒤 Funnel이 켜지는 경우의 주기 점검(A5).

## 7. 정한 것 (2026-10-26, 사용자가 '제안대로'로 정함)

1. Go 모듈 경로는 `github.com/heeo-dev/askorus/agent`. GitHub 조직 이름이 바뀌면 같이 바꾼다.
2. 실행 파일 이름은 `askorus-agent`. 설계 문서의 `agent pair`·`agent devices`·`agent revoke`는 `askorus-agent pair`·`askorus-agent devices`·`askorus-agent devices revoke`다.
3. 설정 위치는 `os.UserConfigDir()/Askorus`(Windows `%AppData%\Askorus`). `agent.json`(설정), `devices.json`(토큰 해시), `logs\agent.log`. `ASKORUS_AGENT_DIR`로 바꿀 수 있다.
4. 제목 순서는 `custom-title` → `<uuid>/custom-title.json` → `agent-name` → `ai-title`.
5. 데스크톱 앱 세션(`entrypoint: claude-desktop`)도 목록에 넣고 `desktop` 배지를 단다.
6. 목록의 `agents` 결과는 2초 동안 그대로, 1분까지는 먼저 돌려주고 뒤에서 새로 받는다. 에이전트가 상태를 바꾸면 바로 지운다.
7. `agents`를 못 읽으면 상태는 `unknown`이고, 이어 보기 명령을 주지 않는다.
8. 위 2–7과 A2의 짝 맺기 세부는 원본 'Phase 1 설계'에도 적었다.

이전에 물었던 것(모두 정해짐, 2026-10-26): serve는 HTTPS 8790으로 설정했다. 멈춘 bg 세션은 `pid`·`status`가 빠지고 `state: stopped`만 남는 것을 확인했다. 폰 시험은 시험용 웹 페이지(`serve -test-page`)로 한다. PC 터미널 문구는 영어와 한국어를 함께 낸다.

## 8. A3 결과 (2026-10-26)

정한 것(사용자):

- **지우기(F5)**: `claude rm`으로 bg 목록에서 빼고, 에이전트의 숨김 목록(`hidden.json`)에 적어 우리 목록에서도 뺀다. 대화 기록 파일은 지우지 않는다. `askorus-agent hidden unhide <id>`로 되돌린다.
- **모델·노력 수준**: 요청이나 설정(`defaultModel`·`defaultEffort`)에 값이 없으면 `--model`·`--effort`를 붙이지 않고 Claude Code 기본값을 쓴다.

만든 것:

- 실행기 허용 목록: `--bg`, `--resume <id>`, `--remote-control [이름]`, `-n <이름>`, `--model`, `--effort`, `stop <id>`, `rm <id>`, `logs <id>`만. 값은 `-`로 시작할 수 없다. 권한 플래그 거부 목록은 그대로 먼저 본다.
- 실행 규칙 5가지(설계 '실행 규칙')와 원격 링크 20초 대기. 재개는 세션의 원래 폴더에서 실행한다(claude가 폴더 기준으로 세션을 찾음). 멈춘 bg 세션은 플래그 없이 재개한다. 멈춘 뒤에는 프로세스가 사라질 때까지 기다린다. 세션마다 한 번에 동작 하나만.
- 'Workspace not trusted'를 잡아 `workspace_not_trusted`(412)로 알린다.
- API: `POST /v1/sessions/{id}/open`, `POST /v1/sessions {folder, name, model, effort}`, `POST /v1/sessions/{id}/stop`, `DELETE /v1/sessions/{id}`, `GET /v1/folders`. 오류 코드와 상태: 없음 404, 진행 중 409, 터미널 아닌 세션 멈추기 409, 상태 모름 503, 신뢰 안 됨 412, 링크 시간 초과 504, 실행 실패 502.
- capability: claude를 찾으면 `list`·`start`·`resume`·`stop`·`remove`·`open_official`.
- 명령줄: `folders [list|add|remove]`, `open`, `new`, `stop`, `rm`, `hidden`. `folders add`는 실행 중인 서버에도 바로 반영된다.
- 시험 페이지: 세션마다 열기·멈추기·지우기(두 번 확인) 버튼, 새 세션 폼.
- 모델 이름 검사가 `-`로 시작하는 값을 받던 것을 고쳤다(실행기에서도 막혔지만 API에서 먼저 막는다).

시험 결과:

- `go vet ./...`, `go test ./...` 통과(10개 패키지). 가짜 claude가 bg 상태를 기억하고(`--bg`, `stop`, `rm`) 원격 링크 줄을 써서, 상태별 인자·작업 폴더·링크 대기·신뢰 오류·시간 초과·동시 동작·숨김을 본다.
- 실제 PC(시험 폴더 `.local\rc-test`):

| 경우 | 결과 | 걸린 시간 |
| --- | --- | --- |
| 새 세션 | `--bg --remote-control asksemble-a3-test -n …`, 원격 링크 받음 | 13.0초 |
| 실행 중 세션 열기 | 아무것도 실행 안 함, 링크만 | 11.7초 |
| 멈추기 | `claude stop`, 멈춤 확인 | 8.4초 |
| 멈춘 세션 열기 | `--bg --resume <id>`(플래그 없음), 같은 id·같은 링크, 복사본 없음 | 17.6초 |
| 기록만 세션 열기 | `--bg --resume <id> --remote-control -n <제목>`, 새 링크 | 18.5초 |
| 터미널 세션 열기 | 아무것도 실행 안 함, `/rc` 안내, agents 수 그대로 | — |
| 신뢰 안 한 폴더에 새 세션 | 신뢰 필요 오류, 세션 안 생김 | 2.2초 |
| 지우기 | 멈춘 뒤 `claude rm`, 숨김 목록에 들어감, 기록 파일 남음 | — |

- 폰 시험에서 찾은 것 두 가지(2026-10-26):
  - 질문에 답을 마친 bg 세션은 pid가 있는 채로 `state: done`이 된다. 처음에는 `done`을 '멈춤'으로 읽어서, 멈추기가 아무것도 하지 않았고 이어진 열기가 살아 있는 세션을 `--resume`해 **복사본**을 만들었다. 이제 pid가 있으면 상태 값과 상관없이 실행 중으로 본다. 멈추기는 pid가 사라질 때까지 기다리고, 재개 직전에 pid가 남아 있으면 재개하지 않는다.
  - 한 번도 질문하지 않은 새 세션은 멈추는 순간 `claude agents`에서 사라진다. 그래서 이어진 `claude rm`이 실패해 숨김까지 가지 못했다. 이미 사라졌으면 `rm`을 건너뛰고 숨긴다.
- 처음 새 세션 결과가 '멈춤'으로 보였다. 링크를 받은 직후 예전 목록을 다시 써서였고, 결과를 만들 때 목록을 새로 받게 고쳤다.

남은 것(A3):

- **속도** → 줄임(2026-10-26): 결과를 만들 때 `claude agents`를 다시 부르지 않고, 새 세션 id는 `claude agents` 대신 기록 파일 이름(짧은 id로 시작)으로 찾는다. 열기 한 번에 `claude agents`는 상태 확인 1번만, 새 세션은 0번(테스트로 고정). 서버 경로(API + 작업)로 잰 값:

| 동작 | 전 | 지금 | Claude 자체 시간 |
| --- | --- | --- | --- |
| 실행 중 세션 열기 | 11.7초(명령줄) | 1.5초 | — |
| 멈춘 세션 열기 | 14.9초 | 14.2초 | 약 12초 |
| 새 세션 | 13~19초 | 12.7초 | 약 12.2초(명령 2.1초 + 원격 연결 약 10초) |

  남은 시간은 Claude가 원격 연결을 붙이는 시간이다. `~/.claude/sessions/<pid>.json`의 `bridgeSessionId`도 기록 파일의 링크 줄보다 0.3초 빠를 뿐이라 쓰지 않는다. 명령줄 명령은 매번 기록 파일을 처음부터 읽어(3~4초) 서버보다 느리다.
- **관리자 권한 서비스 감지**: A5(설치 점검)로 옮겼다(2026-10-26 사용자 결정).
- ~~**폰에서 열기**~~ → 고침(2026-10-26): 폰 시험에서 열기가 PC에서는 성공(14.9초)했는데 폰은 응답 전에 끊겨 "Failed to fetch"가 떴다. 열기와 새 세션은 이제 바로 202와 작업 id를 돌려주고, 폰이 `GET /v1/jobs/{jobId}`로 1초마다 묻는다(`running` → `done`이면 열기 결과, `failed`면 오류 코드). 작업은 시작한 기기만 볼 수 있고, 같은 동작을 다시 보내면 진행 중인 작업에 붙는다. 끝난 작업은 10분 동안 읽을 수 있고, 에이전트가 다시 켜지면 사라진다. 멈추기·지우기는 다시 보내도 안전해서 지금은 그대로 둔다.

## 9. 물어볼 것

지금 물어볼 것은 없다. N5는 "실행 중 세션 열기 2초 안, 깨우기·새 세션은 보통 15초 안(앱은 '켜는 중'을 보여 주며 기다림)"으로 고쳤다(2026-10-26 사용자 결정, 설계 rev 55).

## 10. A4 결과 (2026-10-26)

먼저 확인한 것(Windows 10, Claude Code 2.1.310, 시험 폴더 `.local\rc-test`):

| 끄는 방법 | 결과 |
| --- | --- |
| 숨긴 새 콘솔 + Ctrl+C 신호(`GenerateConsoleCtrlEvent`) | 무시됨. 15초 뒤에도 실행 중 |
| Ctrl+Break 신호 | 0.0초 만에 강제 종료(정리 없음) |
| **가상 터미널(ConPTY)에 Ctrl+C 키(0x03)** | **1.5초 만에 정상 종료, 'Environment preserved'** |

서버 화면은 Ctrl+C를 신호가 아니라 키로 읽는다. 그래서 서버는 가상 터미널 안에서 띄우고, 끌 때 Ctrl+C 키를 보낸다(`golang.org/x/sys/windows`를 새로 씀).

만든 것:

- **서버 관리자**(`adapter/claude/server.go`, `pty_windows.go`): `claude remote-control --name <폴더 이름> --spawn same-dir|worktree --capacity N`을 가상 터미널에서 띄운다. 화면에서 'Connected', 환경 id, `Capacity n/m`만 읽고 나머지는 버린다. 끄면 Ctrl+C 키, 8초 안에 안 끝나면 강제 종료. 스스로 끝나면 2초·10초·30초·1분·5분 간격으로 다시 띄운다.
- **신뢰하지 않은 폴더**: 터미널에서는 서버가 `Trust <폴더>? [y/N]`를 묻고 기다린다(`--bg`처럼 바로 끝나지 않음). 에이전트는 절대 y로 답하지 않고 Ctrl+C로 거절하고 `untrusted`로 알린다. 1분마다 다시 확인한다(그사이 PC에서 신뢰했을 수 있음).
- **다른 서버가 있는 폴더**: `bridge-pointer.json`의 pid가 살아 있고 우리 서버가 아니면 `external`로 두고 건드리지 않는다.
- **설정에 맞추기**: `agent.json`의 폴더별 `server.enabled`가 원하는 상태다. `serve`가 5초마다, 그리고 설정이 바뀌면 바로 서버를 켜고 끈다. 에이전트가 끝날 때 모든 서버에 Ctrl+C를 보낸다. 에이전트가 강제로 죽으면 가상 터미널이 닫히면서 서버도 함께 끝난다(고아 없음).
- **API**: `PUT /v1/folders/{id}/server {enabled, spawn, capacity}`, `GET /v1/folders`에 실제 상태(`status`: off·starting·running·failed·untrusted·external, 세션 수, 환경 id)와 `isGit`. `worktree`는 git 저장소만.
- **명령줄**: `askorus-agent folders serve ID on|off [-spawn] [-capacity]`.
- **서버 세션 표시**: 서버가 만든 세션은 서버 프로세스의 자식이다(실제로 `claude --print --sdk-url …`가 서버 아래에서 돈다). `claude agents`에는 `interactive`로 보이지만, 목록에서는 '실행 중' + `server` 배지로 보이고, 열기를 누르면 `/rc` 안내 대신 링크(없으면 `server_session` 안내)를 준다.
- **capability**: Windows에서 `serve`를 알린다.
- **시험 페이지**: '4. 폴더 열어 두기'에서 켜기·끄기와 상태.
- 로그: 서버 상태가 바뀔 때마다 한 줄(폴더·상태·다시 띄운 횟수·오류).

시험 결과:

- `go vet ./...`, `go test ./...` 통과(11개 패키지). 가짜 claude가 진짜 서버처럼 가상 터미널에서 화면을 그리고, Ctrl+C 키로만 끝나고, 신뢰 질문을 낸다. 시작·정상 종료·같은 옵션 무시·옵션 바뀌면 다시 띄우기·신뢰 안 됨(y를 누르지 않음, 1분 뒤 다시)·스스로 끝나면 다시 띄우기·다른 서버 존중·Ctrl+C 무시 시 강제 종료·서버 세션 표시를 본다.
- 실제 PC(임시 설정 폴더로 띄운 두 번째 에이전트, 포트 8791):

| 시험 | 결과 |
| --- | --- |
| `rc-test` 열어 두기 켬 | 서버가 에이전트 자식으로 뜸, `bridge-pointer.json`에 그 pid, 미리 만든 세션이 서버의 자식으로 돎 |
| 명령줄로 끔 | 4.6초 뒤 서버와 세션 프로세스 모두 끝남(설정 확인 주기 5초 포함) |
| 다시 켬 | 1.1초 만에 서버가 다시 뜸 |
| 에이전트를 강제로 죽임 | 서버도 바로 끝남, 남은 프로세스 없음 |
| 신뢰하지 않은 폴더 | 1초 안에 `untrusted`, 서버 프로세스 안 남음, 15초 동안 다시 띄우지 않음 |

- **폰에서 확인(A4 관문 통과, 2026-10-26 23:05)**: 시험 페이지에서 `rc-test`를 켜자 5초 만에 '열려 있음', Claude 앱의 PC 목록에서 이 폴더를 골라 만든 세션이 서버의 자식으로 떴고, 시험 페이지에 `[폴더 서버]` 배지로 보였다.

세션 파일 읽기(2026-10-26, 사용자 허락):

- `~/.claude/sessions/<pid>.json`은 살아 있는 세션 프로세스마다 하나씩 있고, 원격이 켜져 있으면 원격 세션 id(`bridgeSessionId`)가 적힌다. `--rc` 없이 연 터미널 세션에는 없다. 이름이 `<숫자>.json`인 파일만 읽고, 옆의 `<pid>.<해시>.key` 파일은 열지 않는다(테스트로 고정).
- `--rc`(또는 `/rc`)로 연 터미널 세션: 목록에 링크와 `rc` 배지('원격 켜짐')가 붙고, 열기를 누르면 `/rc` 안내 대신 링크를 준다.
- **`claude agents`가 빠뜨리는 세션**: 실제 PC에서 터미널에 열려 있던 세션 둘(그중 하나는 관리자 권한 터미널)이 `claude agents --json --all`에 없었다. 그래서 '기록만'으로 보였고, 열면 다른 프로세스가 쓰는 세션을 `--resume`할 뻔했다. 세션 파일에 살아 있는 프로세스가 있으면 터미널(또는 bg) 세션으로 보게 고쳤다.
- 새 세션·재개 때 원격 링크도 이 파일에 생기면 바로 쓴다.

남은 것:
- 앱(B1)의 폴더 화면은 이 API를 그대로 쓴다.
- macOS·Linux 서버는 D 단계(지금은 Windows만 `serve`를 알림).

## 11. A5 결과 (2026-10-27)

만든 것:

- **관리자 권한 서비스 감지**(`adapter/claude/daemon.go`): `~/.claude/daemon.lock`의 pid가 살아 있고 그 프로세스가 관리자 권한이면, 열기·새 세션이 45초 기다리지 않고 바로 `service_elevated`(503)로 "관리자 터미널에서 `claude daemon stop --any`"를 안내한다. `/v1/info`에도 알림(`claude_service_elevated`)이 뜬다.
- **관리자로 실행 거부(N3)**: `serve`는 관리자 권한이면 시작하지 않는다. 그 아래에서 켠 세션이 모두 관리자 권한을 갖게 되기 때문이다.
- **백그라운드 실행**: `start`(창 없이 `serve`를 띄우고 끝남), `shutdown`(이름 붙은 이벤트로 멈춤 신호, 포트마다 따로. 에이전트가 폴더 서버를 Ctrl+C로 정리하고 끝날 때까지 기다림), `status`.
- **자동 시작**: `autostart on|off|status`. 이 사용자의 `HKCU\…\Run`에 `"askorus-agent.exe" start`를 넣는다. 관리자 권한이 필요 없고 일반 권한으로 켜진다. 관리자 터미널에서 켜려 하면 거부한다. 로그인할 때 콘솔 창이 잠깐 깜빡일 수 있다(콘솔 프로그램이라서).
- **`check`**: 권한, Claude Code 버전, 관리자 서비스, Remote Control을 막는 환경 변수, Tailscale(로그인 계정, HTTPS 인증서, 에이전트를 가리키는 serve 포트(없으면 비어 있는 포트를 골라 실행할 명령을 보여 줌), Funnel), 에이전트 실행·자동 시작, 짝 맺은 폰, 폴더, 절전 설정, 한 번만 할 일(`claude --rc` 동의, 푸시 설정)을 본다. 읽기만 하고, 고칠 명령은 사람이 실행한다.
- **`uninstall [-yes] [-all]`**: 묻고 나서 에이전트를 멈추고, 자동 시작을 지우고, 짝 맺은 폰을 끊고(`devices.json` 삭제), 에이전트를 가리키는 tailscale serve 포트 하나만 끈다(`tailscale serve --https=<포트> off`). 다른 serve 설정과 Claude Code는 건드리지 않는다. `-all`이면 설정 폴더도 지운다.
- **실행 중 Funnel 점검**: 1분마다 확인해서, 에이전트 포트에 Funnel이 켜지면 멈춘다.
- **절전 막기**: 설정 `"keepAwake": true`이면 bg 세션이나 폴더 서버가 도는 동안 PC가 잠들지 않게 한다(기본 꺼짐).

시험 결과:

- `go vet ./...`(Windows·Linux), `go test ./...` 통과(12개 패키지). 관리자 서비스가 있으면 아무것도 실행하지 않고 실패하는지, uninstall을 거절하면 아무것도 안 바뀌는지, 테스트가 실제 레지스트리를 건드리지 않는지 본다.
- 실제 PC:
  - `check`: 실제 설정을 맞게 읽었다(문제 0개, 주의 1개: 자동 시작 꺼짐). 한국어 Windows의 `powercfg` 출력도 읽었다.
  - 임시 설정 폴더(포트 8791)로 `start` → `status` → 다시 `start`('이미 실행 중') → `shutdown`: 창 없이 떴고, 띄운 쪽 프로세스가 끝나도 계속 돌았고, 0.4초 만에 멈췄다.
  - 폴더 서버를 켠 채 `shutdown`: 1.6초 만에 서버를 Ctrl+C로 정리하고 끝났다. 처음에는 포트만 닫힌 것을 보고 "멈췄다"고 일찍 말해서, 에이전트가 pid 파일을 쓰고 `shutdown`이 그 프로세스가 끝날 때까지 기다리게 고쳤다.
- 실제 PC에서 하지 않은 것: `autostart on`(레지스트리 등록), 실제 설정으로 `uninstall`(serve 8790을 끔). 사용자 확인 뒤에 한다.

## 12. 정한 것 (2026-10-27)

- 이 PC에 자동 시작을 등록했다: `autostart on -test-page` → `"…\.local\bin\askorus-agent.exe" start -test-page`. 폰 앱(B1)이 나오기 전까지 시험 페이지를 쓰려고 `autostart on`에 `-test-page`를 더했다.
- 이 세션이 띄웠던 에이전트를 멈추고 A5 빌드를 `start -test-page`로 다시 켰다. 사설망 주소에서 시험 페이지 200, API는 토큰 없이 401. `check`는 주의 0개.

## 13. B1 계획 (2026-10-27)

`app/`에 Expo SDK 58(React Native 0.88, TypeScript, Expo Router, 화면은 `src/app/`). 개발 중에는 폰의 Expo Go가 PC의 개발 서버(Metro 8081)에 Tailscale 주소로 붙는다. PC에 Android SDK·JDK는 깔지 않고, 설치 파일은 B2에서 EAS 클라우드로 빌드한다(Expo 계정은 사용자가 만들었다).

| 단계 | 할 일 |
| --- | --- |
| B1-1 뼈대 | Expo 프로젝트, ko/en 문구, `/v1` 클라이언트(작업·짝 맺기 다시 묻기, 끊긴 요청 다시 묻기), 연결 목록(`kind: pc`, 토큰은 보안 저장소에 따로), 단위 테스트 |
| B1-2 짝 맺기 | Tailscale 안내, QR 스캔(`expo-camera`), `askorus://pair`와 시험 페이지 링크 둘 다 읽기, PC 허용 기다리기 |
| B1-3 세션 목록·상세 | 검색, 상태 필터, 배지, 당겨서 새로 고침. 상세 시트: 열기(작업 id), 멈추기, 지우기 재확인, 이어 보기 명령 복사, 터미널·서버 세션 안내 |
| B1-4 새 세션·폴더·설정 | 폴더·이름·모델·노력 수준, 폴더 열어 두기(F10), PC 목록과 끊기, 연결 진단 |
| B1-5 오류·마무리 | 원인별 오류 화면, 여러 PC(F7), '한 번에 묻기' 탭 자리(숨김), 공식 제품 아님 문구 |

관문: 시험 페이지로 되던 것이 실제 폰의 앱에서 모두 되고, PC 두 대가 섞이지 않는다.

알아 둘 것:

- 열기 링크: 앱 안에서 `Linking.openURL(https://claude.ai/code/session_…)`이 Claude 앱으로 바로 가는지 실제 폰에서 먼저 본다. 안 되면 Android는 패키지(`com.anthropic.claude`)를 지정한다.
- 에이전트 쪽: 설정의 '끊기'가 PC의 기기 정보도 지우려면 `DELETE /v1/devices/self`가 필요하다(필드·경로를 더하기만).

B1-1 결과: `npx tsc --noEmit` 통과, `npx jest` 4개 묶음 40개 통과(클라이언트, QR 링크, 연결 저장, 문구), `npx expo-doctor` 20/20. Android 묶음이 오류 없이 만들어진다(1295개 모듈). 테스트 파일은 `*-test.ts`이고, Windows에서 `<rootDir>`가 든 testMatch가 파일을 못 찾아 `**/src/**/*-test.[jt]s?(x)`로 적었다. 템플릿의 `app/.claude/settings.json`(Expo 플러그인 켜기)과 MIT `LICENSE`는 지웠다.
- 실제 폰(2026-10-27, S23): Expo Go에서 `exp://<PC의 Tailscale 주소>:8081`로 열려 '내 PC' 첫 화면이 보였다. Windows 방화벽은 따로 손대지 않아도 됐다.
- B1-2 실제 폰(2026-10-27): PC에서 `pair` 창을 띄우고 앱으로 QR을 찍어 짝 맺었다. PC에서 y로 허용한 뒤 첫 화면에 PC가 '연결됨'으로 떴다. PC 기기 목록에 폰 이름(Android 기기 이름)으로 새 기기가 생겼고, 시험 페이지 기기와 따로 남는다.
- B1-3 실제 폰(2026-10-27): 목록이 보였고, 열기를 누르면 앱 안의 `Linking.openURL(https://claude.ai/code/session_…)`이 Claude 앱으로 바로 갔다(Android에서 패키지 지정 필요 없음). '기록만' 세션 하나가 `launch_failed`로 열리지 않았다. 원인은 그 세션의 폴더(구현 전 테스트 7의 임시 폴더)가 지워진 것이었다. 에이전트에 `session_folder_missing`(410) 오류와 `folder_missing` 배지를 더했다(필드·값 더하기만). 목록을 만들 때 멈춤·기록만 세션의 폴더가 없으면 배지를 달고 `openModes: []`, 이어 보기 명령을 비운다. 앱과 시험 페이지는 열기 버튼을 숨기고 이유를 보여 준다. 이 PC에서는 68개 중 3개(테스트 7·8 임시 폴더)가 걸렸다.
- 화면 테스트(2026-10-27): `src/test/screens-test.tsx`가 실제 화면(`src/app`)을 Expo Router의 `renderRouter`로 띄우고, 가짜 에이전트(`src/test/fakeAgent.ts`)를 붙여 사람처럼 누른다. 첫 화면, 짝 맺기(붙여넣기·딥 링크·거절·잘못된 링크), 세션 목록·필터·검색, 열기→Claude 앱 링크, 멈추기, 지우기 확인, 폴더 없음, 짝 끊김을 본다(15개, 전체 62개 통과). 테스트 파일은 `src/app` 밖에 둔다(그 안의 파일은 화면이 된다). 실제 폰 조작은 adb 무선 디버깅으로 한다(platform-tools 37.0.1, Google 서명 확인, `%USERPROFILE%\sdk\platform-tools`).
- B1-4(2026-10-27): 새 세션(`/pc/[id]/new`: 허용 폴더, 이름, 모델 기본값·opus·sonnet·haiku, 노력 수준 기본값·low…max, 시작하면 Claude 앱으로 넘김), 폴더(`/pc/[id]/folders`: 폴더마다 열어 두기 스위치, 상태·세션 수, 켜는 중이면 2초마다 1분까지 다시 묻기), 설정(`/settings`: PC 목록, 연결 진단, 끊기, 새 세션 기본값, 공식 제품 아님). 에이전트에 `DELETE /v1/devices/self`를 더했다(폰이 스스로 짝을 끊음, 다른 기기는 그대로). PC에 닿지 않을 때 끊으면 폰에서만 지우고 PC에서 지울 명령(`devices revoke <id>`)을 보여 준다. 화면 테스트 10개를 더해 전체 72개 통과, Go 테스트 모두 통과. Expo Router의 경로 타입 생성(typedRoutes)이 `src/app` 밖 파일까지 경로로 넣어 꺼 두었다. RNTL 14의 `fireEvent`는 Promise라 반드시 기다린다(안 기다리면 다음 테스트가 멈춤).
- 기록 폴더(2026-10-27 사용자 결정, 설계 rev 61): 새 세션 폴더를 PC 허용 목록뿐 아니라 대화 기록이 있는 폴더에서도 고른다. 기본으로 켜 두고 `agent.json`의 `"historyFolders": false`로 끈다. 에이전트는 기록의 작업 폴더 중 지금 있는 것을 최근 순으로 30개까지 `GET /v1/folders`에 `source: "history"`로 더한다(`source`, `lastActive` 필드를 더함). 워크트리(`.claude/worktrees` 아래), 숨긴 폴더, 이미 허용된 폴더는 뺀다. id는 경로의 SHA-256 앞 12자리에 `h-`를 붙인 값이고, 폰은 이 id로만 고른다(경로를 보내면 404). 기록 폴더에서 열어 두기를 켜면 허용 목록에 저장(`AddFolder`)하고 서버를 켠다. 끄기는 아무것도 바꾸지 않는다. 앱의 새 세션 화면은 'PC에서 허용한 폴더'와 '최근 대화한 폴더'를 나눠 보여 주고, 폴더 화면에도 최근 폴더를 따로 둔다. 이 PC에서는 기록 폴더 5개(rc-test는 이미 허용돼 4개가 새로 보임). Go 테스트(설정·어댑터·API)와 앱 테스트 74개 통과.
- B1-5(2026-10-27): 접속 실패를 원인으로 나눈다(`src/connections/reach.ts`). 401은 짝 끊김(다시 짝 맺기 버튼), 403 wrong_account는 다른 Tailscale 계정, serve가 대신 답한 502·503·504는 'PC는 켜져 있지만 에이전트가 꺼짐'(`askorus-agent start`·`autostart on` 안내), 바로 실패한 연결은 폰 쪽(Tailscale·VPN)부터, 시간 초과는 PC 쪽(꺼짐·잠듦·PC의 Tailscale)부터 확인할 것을 번호로 보여 준다. 폰은 사설망 안을 볼 수 없어 추정이므로 나머지 원인도 같이 둔다. 세션 목록에 오류 상자와 '다시 시도', 첫 화면 카드에 한 줄, 설정의 연결 진단에 같은 안내. PC가 알리는 문제(`/v1/info`의 notices: 관리자 서비스, Remote Control을 막는 환경 변수 등)를 목록 위에 띄운다. 두 번째 입구 '한 번에 묻기'는 `src/app/ask.tsx` 자리만 두고 `features.ask=false`로 숨긴다(원칙 9). 여러 PC(F7)는 PC마다 토큰이 따로 쓰이는지 화면 테스트로 본다. 앱 테스트 83개 통과, expo-doctor 20/20.

## 14. 재부팅 뒤 '보관됨' (2026-10-27)

재부팅 뒤 터미널에서 이 세션을 /resume, /rc로 쓰고 /exit한 다음 앱에서 열었더니 Claude 앱에 '보관된 세션'이 떴고, 보관을 푼 뒤에는 '끊김'이 떴다.

- 에이전트 판단은 맞았다: 터미널을 닫은 뒤라 세션은 꺼져 있었고(`claude agents`: bg, pid 없음, `done`), 플래그 없이 `--bg --resume`했다.
- 백그라운드로 다시 켜면 Claude는 그 대화의 예전 원격 링크에 다시 붙는다. 그 링크는 다시 붙기 전까지 보관 상태다. 다시 붙는 데 9~19초 걸렸다.
- 에이전트는 다시 켠 뒤 3초가 지나 프로세스가 돌면 기록 파일의 예전 링크를 돌려주는 지름길이 있었다. 그래서 앱이 아직 보관 상태인 링크를 열었다. 이 지름길을 없애고, 다시 켠 뒤에 쓰인 링크 줄(또는 세션 파일의 링크)만 받는다. 기다리는 시간은 20초에서 45초로 늘렸다.
- 앱 시트의 '다시 열기'는 예전 링크를 그대로 열었다. 멈춘 뒤 누르면 멈춘 세션의 링크라 '보관됨'이 뜨고 Claude 앱 원격 목록에도 없었다(`resume-test`로 재현). 이제 '다시 열기'도 매번 에이전트에 묻고, 멈추면 링크를 지운다.
- 시험으로 알게 된 것: 멈춘 bg 세션에 `--remote-control -n`을 붙여 재개하면 새 세션 id의 복사본이 생긴다(설계의 실행 규칙 그대로). `claude rm` 뒤에 플래그를 붙여 재개하면 복사본은 없지만 링크는 예전 것을 쓴다. 프로세스만 강제로 끄면 Claude 서비스가 곧 다시 띄운다. `claude daemon stop --any`는 bg 세션을 모두 끈다(ETF-AI 세션 포함, 사용자 허락 뒤 시험).
- 시험 세션 `resume-test`(rc-test)와 시험 중 생긴 복사본 `79ecd11b`(목록에서 숨김)가 남아 있다.

- 10-28 확인(E2E-07, PC Chrome): `claude stop`만 해도 Claude가 원격 세션을 보관하고 Code 목록에서 뺀다. 에이전트로 다시 깨우면 연결이 붙으면서 보관이 저절로 풀리고 목록에 '연결됨'으로 돌아온다. 재부팅 때의 '보관됨'은 앱이 다시 붙기 전에 링크를 연 탓이었고, 링크를 늦게 주는 고침(3ea3ddc)으로 해결됐다. 에뮬레이터는 Google Play 이미지 + Tailscale 앱으로 바꿨다(Claude 앱은 Play 프로텍트 미인증이라 로그인 불가).

## 15. B2 계획 (2026-10-28)

설치 파일은 EAS 클라우드에서 빌드한다(PC에 Android SDK·JDK를 깔지 않음, 13절). 빌드 설정과 출시 규칙은 `docs/android-release.md`에, 누가 무엇을 하는지는 작성자의 로컬 메모(`.local/play-console.md`)에 있다.

| 단계 | 할 일 |
| --- | --- |
| B2-1 빌드 설정 | `eas.json`(preview APK, production AAB, 빌드 번호는 EAS가 보관), 패키지 이름, 쓰지 않는 권한 빼기, 설치본에서만 드러나는 설정을 UT로 지키기 **(완료)** |
| B2-2 첫 빌드 | 사용자가 Expo에 로그인한 뒤 `eas init`, preview APK를 폰에 설치해 MT-01~08 |
| B2-3 Play | 개발자 등록, 앱 만들기, 서비스 계정 키, production AAB를 내부 테스트에 올리기 |
| B2-4 1주일 사용 | Play에서 받은 앱으로 매일 쓰고, 문제는 회귀 표에 적고 고친다 |

B2-1 결과:

- `npx expo prebuild`로 Android 프로젝트를 만들어(빌드는 하지 않고 확인 뒤 지움) 매니페스트를 봤다. Expo가 기본으로 넣는 저장소 읽기·쓰기, 다른 앱 위에 그리기, 진동 권한을 `blockedPermissions`로 뺐다. 남는 권한은 카메라와 인터넷이다.
- prebuild가 `userInterfaceStyle`에는 `expo-system-ui`가 필요하다고 경고했다. 없으면 설치본은 늘 밝은 화면이다. Expo Go에서는 보이지 않던 문제라 `expo-system-ui`를 더하고 `src/config-test.ts`로 지킨다(R-14).
- SDK 58의 새 패치(`expo` 58.0.20 등)로 맞췄다(expo-doctor 20/20).
- 패키지 이름은 `app.askorus`(2026-10-28 사용자 확정).

B2-2(2026-10-28): `eas init`으로 `@heeodev/asksemble`을 만들었다. 첫 preview 빌드 때 EAS가 서명 키를 클라우드에서 만들었고(PC에 keytool 없음), versionCode는 1에서 시작한다. 올린 파일은 1.6MB다(`.local`·비밀값은 gitignore로 빠짐). 빌드는 19분 만에 끝났다(0.1.0, versionCode 1). 다음은 폰에 설치해 MT-01~08.
- 개인정보처리방침 초안 `docs/privacy.md`(한국어·영어). 공개 주소는 아직 없다.

## 16. PATH (2026-10-28)

`.local\bin`이 PATH에 없어 터미널에서 `askorus-agent pair`가 안 됐다(사용자 지적). 이 PC는 사용자 PATH(HKCU)에 `.local\bin`을 넣었고, 배포판에서도 되도록 `path` 명령을 더했다. 설계의 'PC 설치 흐름' 5번과 지우기에 PATH를 더했다(rev 63).

- `askorus-agent path on|off|status`: 이 exe가 있는 폴더를 이 사용자의 PATH(`HKCU\Environment`의 `Path`)에 넣고 뺀다. 관리자 권한이 필요 없고 시스템 PATH는 건드리지 않는다. 다른 항목은 순서와 적힌 그대로 두고, 값의 종류(REG_SZ·REG_EXPAND_SZ)도 지킨다. 같은 폴더를 대소문자·끝의 `\`·따옴표·`%LOCALAPPDATA%`가 달라도 같은 것으로 본다. 바꾼 뒤 Windows에 알려(WM_SETTINGCHANGE) 새로 연 터미널부터 적용된다.
- exe가 내려받기 폴더나 임시 폴더에 있으면 계속 둘 폴더로 옮기라고 알린다.
- `check`는 PATH에 없으면 주의로 `path on`을 안내하고, `uninstall`은 PATH 항목도 뺀다.
- 배포판 설치 순서(D 단계의 설치 문서에 쓴다): exe를 `%LOCALAPPDATA%\Programs\Askorus`에 둔다 → `askorus-agent.exe path on` → 새 터미널에서 `askorus-agent autostart on`, `check`, `pair`. 2026-11-09(#37): `scripts/install.ps1`(배포판의 `install.ps1`, `irm … | iex` 한 줄)이 exe를 받아 SHA-256을 보고 이 폴더에 둔 뒤 `askorus-agent setup`을 돌린다. setup은 위 순서를 묶은 것이다: 권한·폴더·PATH → AI 프로그램(Claude Code·Codex·OpenCode 중 하나 이상. 2026-11-10 사용자 결정으로 Claude Code는 꼭 있지 않아도 됨: 없으면 '내 PC' 세션에는 필요하다고 알리고 Claude Code 팁은 빼며, 하나뿐이면 종합 답변·토론은 둘 이상일 때라고 알림, 하나도 없을 때만 멈춤) → Tailscale(HTTPS, serve가 안 됐으면 설정을 보이고 [y/N]으로 물어 y면 setup이 에이전트 포트 하나만 켬, 그냥 Enter면 직접 실행하고 Enter. 2026-11-09 사용자 결정) → 켜기·로그인할 때 켜기 → 짝 맺기(짝 맺은 기기가 없을 때). 다시 돌리면 된 단계는 건너뛴다. 스크립트를 다시 돌리면 업데이트(돌던 에이전트를 멈추고 바꾼 뒤 다시 켬, 옛 exe는 `askorus-agent.prev.exe`). 배포 파일은 `scripts/release.ps1`(판 번호, -trimpath, exe 안 경로 검사, `SHA256SUMS.txt`). 나중에 winget(portable)으로 내면 winget이 PATH를 맡는다.
- 시험: `pathenv_test.go`(문자열만, 실제 PATH는 안 건드림), uninstall 테스트는 PATH 빼기를 가짜로 바꾼다. 실제 PC에서 임시 폴더의 빌드로 on → on(이미 있음) → status → off를 돌려 PATH가 원래 값 그대로 돌아온 것을 봤다.
- `.local\bin`에 배포(2026-10-28): 열어 둔 폴더 서버가 없는 것을 보고 `shutdown` → 교체 → `start -test-page`. `check` 주의 0개(PATH ok), Tailscale 주소의 `/v1/info`는 토큰 없이 401. 예전 빌드는 `askorus-agent.prev.exe`로 남겼다.

## 17. 열어 두기를 꺼도 Claude 앱에 폴더가 남음 (2026-10-28, MT-06)

설치본으로 MT-06을 하다 찾았다. 폴더 열어 두기를 꺼도 Claude 앱의 PC 목록에 그 폴더가 남고, 거기서 만든 새 세션('테스트 안녕')이 '연결 중…'에 머물렀다.

- 에이전트는 서버를 제대로 껐다(Ctrl+C, 남은 프로세스 없음). Claude Code(2.1.312)는 일반 종료 때 늘 환경을 지우지 않고 오프라인으로만 표시한다(`Environment preserved`). 지우는 것은 오류나 조직 정책으로 끝날 때뿐이고 고르는 옵션은 없다.
- 끈 뒤 약 4시간(`BRIDGE_POINTER_TTL_MS` 14,400,000) 안에 그 폴더에서 다시 켜면 같은 환경에 다시 붙는다. 실제로 다시 켜자 10초 안에 기다리던 세션이 시작됐고, 처음 켤 때 미리 만든 세션도 새로 만들지 않고 다시 붙었다. 4시간이 지나면 새 환경으로 켜져 기다리던 세션은 시작되지 않는다(실행 파일의 코드로 읽은 것이고 4시간을 기다려 보지는 않았다). 끌 때 돌던 세션을 이어 주는 시간은 15분(`PERSISTED_SESSION_RESUME_WINDOW_MS`)이다.
- 미리 만든 세션을 없애려고 `--no-create-session-in-dir`를 시험했다. 이 폴더에 예전 서버의 `bridge-pointer.json`이 있으면 'This folder is already served by a terminal `claude remote-control` on this device'로 바로 끝나서 켜지지 않았다(세 번 모두). 옵션 없이는 같은 상황에서 바로 켜졌다. 그래서 쓰지 않는다(사용자가 고른 2번을 되돌림).
- 앱 폴더 화면 위에 꺼질 때의 동작을 적고(약 4시간), 방금 끈 폴더 카드에 'Claude 앱 목록에는 아직 남아 있음'을 띄운다(R-15). 두 번째 preview APK(빌드 d25950af)를 폰에 덮어 설치해 화면 위 문구가 보이는 것을 사용자가 확인했다(10-28). 끈 직후 카드 문구는 19:31 확인 뒤에 본다.
- claude.ai에서 본 것: 15:18에 끄고 15:30까지는 목록에 그대로(표시 없음), 16:07에는 같은 목록에 '오프라인' 표시와 새 세션 화면의 '기기가 오프라인 상태입니다' 경고. 다른 원격 세션은 계속 '연결됨'.
- 19:31(끈 지 4시간 13분, 그사이 다시 켜지 않음, 포인터 환경 id 그대로): 환경 목록에서 'Remote Control' 항목이 통째로 없어졌고 `/code`는 GitHub 연결 안내로 넘어가며 기본 환경이 `Default`(클라우드)로 바뀌었다. 세션 '테스트 안녕'·`rc-test`는 목록에 'Remote Control · 오프라인'으로 남고, 열면 '다시 연결되면 이 세션이 재연결됩니다'. 사라진 시각은 16:07과 19:31 사이라 4시간과 맞는지는 확정하지 못했다. 이에 맞춰 화면 위 문구에 '약 4시간 뒤 Claude 앱 목록에서도 빠진다'를 더했다.

## 18. 정한 것: 배포는 미루고 2차·Phase 2로 (2026-10-28)

사용자 결정: Windows PC + Android 폰으로 개발을 먼저 하고, 배포 계획은 Phase가 더 지난 뒤 생각한다. 설계 '단계별 계획'에 반영(rev 64).

- 미룸: B2-2의 남은 확인(MT-05 재부팅, MT-08 기본 카메라 QR), B2-3 Play 등록·업로드, B2-4 1주일 사용 관문, C(iOS), D(macOS·Linux, 서명·공증), 공개.
- 다음: E(2차: F8, F9, `tsnet`) → Phase 2.
- 앱은 지금처럼 EAS preview APK를 폰에 바로 설치해 쓴다. 개인 계정의 비공개 테스트 조건(12명, 14일)은 `docs/android-release.md`에, Play 등록 절차는 작성자의 로컬 메모(`.local/play-console.md`)에 둔다(2026-11-10 나눔).
- 테스트 단계(`docs/testing.md`)는 그대로 지킨다. MT는 단계를 마칠 때 실제 폰에서.

## 19. F8 최근 대화 미리보기 계획 (2026-10-28)

설계 F8: 세션의 마지막 질문과 답 몇 줄을 읽기 전용으로 보여 준다. 폰에서 세션을 열기 전에 "이게 무슨 세션이었지"를 알게 하는 것이 목적이다.

| 단계 | 할 일 |
| --- | --- |
| F8-1 에이전트 | 기록 파일 끝에서 거꾸로 읽어 마지막 질문·답을 뽑는 읽기 함수(어댑터 안), `GET /v1/sessions/{id}/preview`, capability `preview` |
| F8-2 앱 | 세션 상세 시트에 '최근 대화' 영역(시트를 열 때 불러옴, 못 불러오면 조용히 숨김), ko/en 문구 |
| F8-3 시험 | UT·IT·ST, test-live(rc-test 세션), E2E 흐름 하나, MT |

뽑는 규칙(기록 형식은 10-28 rc-test 기록으로 확인, 줄 종류와 블록 종류만 셈):

- 질문 = `type: user`이고 `message.content`가 글자인 줄. 도구 결과(`tool_result`), `isMeta`, `isSidechain`, 명령 출력(`<command-…>`, `<local-command-…>`로 시작) 줄은 뺀다.
- 답 = 그 질문 뒤 `type: assistant` 줄들의 `text` 블록. `thinking`은 보내지 않는다. `tool_use`는 내용 없이 "도구를 n번 썼음"만 센다.
- 큰 기록(이 PC에서 가장 큰 것 약 38MB)도 끝에서 최대 1MB만 읽는다. 쓰는 중인 마지막 줄은 읽을 수 있으면 쓴다(목록 수집기와 같은 규칙).
- 길이: 질문 300자, 답 600자에서 자르고 `truncated`를 단다. 응답 예: `{"turns":[{"role":"user","text":"…","at":"…"},{"role":"assistant","text":"…","tools":2}],"truncated":false}`.

지킬 것:

- 대화 내용이 밖(폰)으로 나가는 첫 API다. 짝 맺은 기기 토큰이 있어야 하고, 응답 본문은 로그에 남기지 않는다(logx가 이미 `content`·`prompt` 키를 지움. 미리보기 응답 키도 테스트로 고정). 폰은 화면에만 보이고 저장하지 않는다.
- 로그인 정보 파일은 여전히 열지 않는다(N10). 읽는 것은 지금도 읽는 기록 파일뿐이다.
- `/v1`에 경로와 capability를 더하기만 한다(원칙 4). 예전 앱은 모르는 capability를 무시한다.

정한 것(2026-10-28, 사용자가 추천대로): 마지막 질문 1개 + 그 답, 세션 시트에만, 도구 사용 횟수는 보임.

F8-1·F8-2 결과(2026-10-28):

- 에이전트: `core.Previewer`·`core.Preview`(`internal/core/preview.go`), capability `preview`(claude 없이도 기록만 읽으므로 늘 알림), `GET /v1/sessions/{id}/preview`(짝 맺은 기기만, `Cache-Control: no-store`, 요청 로그에는 경로·상태만). 읽기는 `adapter/claude/preview.go`: 기록 폴더에서 `<uuid>.jsonl`을 찾아 끝 1MB만 읽는다.
- 실제 기록 모양을 세어 보니(내용은 보지 않음) 끝부분 사용자 줄은 대부분 도구 결과였고, 백그라운드 작업 알림(`<task-notification>`)도 사용자 줄로 기록된다. 사람이 쓴 게 아니라 질문에서 뺀다.
- 앱: 세션 시트에 '최근 대화'(질문 · Claude · 도구 n번 사용 · 아직 답이 없음). 있으면 예전 '마지막 질문' 줄을 대신하고, 못 읽거나 예전 에이전트면 예전처럼 보인다. 폰에 저장하지 않는다.
- 도구 종류(10-29, 사용자 요청): 횟수만으로는 뜻이 안 와닿아 종류별로 묶는다. 에이전트는 도구 이름(입력·결과는 보지 않음)을 코드로 바꿔 `toolKinds`(많은 순)로 보낸다: read(Read), edit(Edit·Write 등), run(Bash·PowerShell 등), search(Grep·Glob), web(WebFetch·WebSearch), agent(Task), other(MCP 도구 등 나머지). 앱은 "도구: 명령 실행 3 · 파일 읽기 2"로 보이고, 모르는 코드는 코드 그대로, `toolKinds`가 없는 예전 에이전트는 "도구 n번 사용"으로 보인다.
- 시험: UT(`preview_test.go` 6개, API `preview_test.go` 2개), IT-10, LT-04(실제 기록 40개, 가장 느린 것 16ms), ST `preview-test.tsx` 4개. E2E 흐름은 아직 없다(시험 세션은 질문이 없어 볼 것이 없음). MT는 다음 APK에서.

## 20. 정한 것: F9 미룸, Phase 2로 (2026-10-29)

- F9(우리 쪽 알림)는 미룬다. 알릴 대표 사건인 에이전트 꺼짐·PC 잠듦 때는 에이전트가 푸시를 보낼 수 없고, Expo 푸시는 N1(외부 요청 없음)을 깨며 Expo Go로 확인할 수 없다. 하게 되면 폰이 백그라운드에서 PC를 확인해 로컬 알림을 띄운다(설계 rev 65).
- `tsnet`도 배포와 함께 미룬다. 다음은 Phase 2(`docs/phase2-plan.md`). Phase 3를 먼저 하지 않는 이유는 그 문서 1절.

# 테스트

Asksemble Phase 1의 테스트 기준이다. 버그를 고치면 그 버그를 다시 일으키는 테스트를 같이 넣고, 아래 '회귀 표'에 적는다.

## 1. 단계

테스트는 피라미드 모양으로 둔다. 아래쪽(빠르고 안정적)을 많이, 위쪽(느리고 잘 깨짐)을 적게. 기능을 만들 때는 1단계로 빠르게 돌고, 2·3단계는 정해진 때만 돈다.

| 단계 | 무엇을 보나 | 어디 | 실행 | 언제 | 걸리는 시간 |
| --- | --- | --- | --- | --- | --- |
| **1. 매번** | UT: 함수·패키지 하나(에이전트·앱). IT: 에이전트 전체를 가짜 `claude`로 폰 순서대로. ST: 앱 화면 전체를 가짜 에이전트로 사람처럼 | `agent/**/*_test.go`, `scenario_it_test.go`, `app/src/**/*-test.ts(x)` | `scripts\test-all.ps1` | 고칠 때마다, 합치기·배포 전 | 2~5분, 에뮬레이터 없음 |
| **2. 실제 연동** | LT: 에이전트 + **실제 `claude`** + 실제 `~/.claude`, HTTP로 폰 순서대로. E2E-07: claude.ai 쪽 상태(목록·연결됨·보관됨) | `agent/internal/live/live_test.go`(태그 `live`), PC Chrome | `scripts\test-live.ps1`, E2E-07은 Claude in Chrome | 에이전트가 claude를 다루는 부분을 바꿨을 때 | 1~2분 |
| **3. 앱 전체** | E2E: 테스트 폰(무선 디버깅)의 Expo Go 앱 → Tailscale → 실제 에이전트 → 실제 `claude`. Android에만 있는 것(뒤로 가기, 링크 열기, 키보드, 화면 배치)과 앱·에이전트 연동 | `app/e2e/*.yaml`(Maestro) | `scripts\e2e.ps1` | 기본은 건너뜀. Phase 중간에 한두 번(10-29 사용자 결정) | 약 3분 |
| **4. 실제 폰** | MT: 카메라 QR, Claude 앱으로 넘어가기, 폰 Tailscale on/off, PC 재부팅. 한 번만 하지만 꼭 봐야 하는 시험(P0-4 공유 등) | 5절 | 테스트 폰은 제가 `scripts\phone.ps1`로, 카메라·PC 재부팅·S23은 사람이 | 단계(B1, B2 …)를 마칠 때, 시험이 생길 때 | - |

- 가짜 `claude`: 테스트 실행 파일이 `ASKSEMBLE_FAKE_CLAUDE=1`일 때 `claude`처럼 동작한다(`fake_test.go`). 받은 인자를 기록하고 `agents --json`, `--bg`, `stop`, `rm`, `remote-control`, `-p`(묻기, stdin까지 기록)를 흉내 내며, 환경 변수로 신뢰 안 됨·링크 없음·rm 한 번 실패·모르는 모델 같은 상황을 만든다. 가짜 `codex`·`opencode`도 같은 방식이다(`codex_test.go`, `opencode_test.go`).
- 가짜 판정 AI(P2-3, `agent/internal/fakecli`): 테스트 실행 파일이 `ASKSEMBLE_FAKECLI=claude|codex|opencode`(또는 `auto`: 실행 파일 이름 `claude.exe`·`codex.exe`·`opencode.exe`로)일 때 그 AI처럼 동작한다. 인자 모양으로 `--version`·묻기·판정·OpenCode `session list`·`session delete`를 가르고, 호출마다 인자·폴더·환경(`PWD`, `OPENCODE_CONFIG_CONTENT`)·stdin·Codex 스키마 파일 내용을 파일 하나씩 기록한다(`--version`은 기록하지 않음). 판정 모드 `ASKSEMBLE_FAKE_JUDGE`: `rule`(기본. 'NUM n'이 다른 'NUM m'과 만나면 근거 있는 opposite, 같은 수면 same, 없으면 absent), `unsure`, `usage_limit`, `prose`, `bad_json`, `missing_id`, `tool_use`, `slow`(`ASKSEMBLE_FAKECLI_SLEEP`, 기본 60초. OpenCode는 판정 글을 낸 뒤 기다림), `bad_model`, `failed_with_output`. AI마다 `codex=usage_limit,claude=prose_once`처럼 주고, `_once`는 그 AI의 첫 판정에만 쓴다. 판(`ASKSEMBLE_FAKECLI_VERSION`, 예 `codex=0.163.0`)과 OpenCode 세션 명령 실패(`ASKSEMBLE_FAKECLI_FAIL`: `list`·`listjunk`·`delete`·`delete=<id>`)도 만든다. 어댑터는 `TestMain` 첫 줄에서 `fakecli.Main`을 부르고 `fakecli.Use(t, "codex")`가 준 실행 파일을 쓴다. 에이전트 실행 파일에는 들어가지 않는다(`TestNotInAgent`).
- 가짜 에이전트(앱): `app/src/test/fakeAgent.ts`. 실제 에이전트처럼 서버 상태가 한 박자 늦게 바뀐다(R-12).
- 시험 페이지(`/test/`)는 앱이 나오기 전 손으로 보던 도구다. 앱과 코드가 따로라 앱 버그(R-09, R-12)는 못 잡는다. 테스트 단계로 쓰지 않는다.

## 2. 회귀 규칙

1. **고칠 때마다·main에 합치기 전·`.local\bin`에 배포하기 전**: `scripts\test-all.ps1`이 `ALL PASSED`. gofmt, `go vet`(Windows·Linux), Go 전체(UT+IT), 앱 타입 검사, jest 전체(UT+ST), expo-doctor(`-Quick`이면 생략).
2. **에이전트가 claude를 다루는 부분(실행 인자, 상태 판정, 링크, 폴더 서버)을 바꾸면**: `scripts\test-live.ps1`이 `LIVE PASSED`. claude.ai 쪽 동작이 걸리면 E2E-07도 본다.
3. **앱을 바꾼 것을 main에 합치기 전**: `scripts\e2e.ps1`이 모두 Passed(테스트 폰). 에뮬레이터로 돌릴 때는 `-Emulator`.
4. **버그를 고치면**: 가능하면 실패하는 테스트를 먼저 만들고, 고친 뒤 통과를 보고, 회귀 표에 한 줄 더한다. 그 버그를 잡은 단계보다 아래 단계에서 재현할 수 있으면 아래에 넣는다(예: E2E가 잡은 R-12·R-13도 ST·UT로 재현).
5. 테스트 파일은 `app/src/app` 안에 두지 않는다. 그 안의 파일은 Expo Router가 화면으로 묶는다.

## 3. 시나리오 목록

### IT (에이전트, `scenario_it_test.go`)

| ID | 시나리오 | 요구사항 |
| --- | --- | --- |
| IT-01 | 짝 맺기 전 401 → 짝 맺기(PC 허용) → `/v1/info` → 세션 목록 상태 4가지 → 상태 필터 | N4, F1, F2 |
| IT-02 | 멈춘 세션 깨우기 → 실행 중 → 멈추기 → 다시 깨우기, 재개에 플래그 없음(복사본 금지) | F3, F5 |
| IT-03 | 기록만 있는 세션 열기 → `--remote-control -n 제목`으로 재개 | F3 |
| IT-04 | 재부팅 뒤 꺼진 세션: 다시 붙기 전에는 예전 링크를 주지 않고 시간 초과 | F3, R-08 |
| IT-05 | 터미널 세션 열기 → 아무것도 실행하지 않고 `/rc` 안내 | F3, F6 |
| IT-06 | 새 세션: 허용 폴더와 기록 폴더(`h-` id)에서 시작, 경로를 직접 보내면 404 | F4, N2 |
| IT-07 | 지우기 → 목록에서 빠짐 | F5 |
| IT-08 | 신뢰 안 된 폴더 → `workspace_not_trusted`, 폴더가 지워짐 → 열기 버튼 없음·`session_folder_missing` | F3, R-06 |
| IT-09 | 다른 Tailscale 계정 403, 폰이 스스로 짝 끊기 → 그 토큰 401 | N4 |
| IT-10 | 미리보기: 짝 맺기 전 401 → capability `preview` → 기록 세션의 마지막 질문 → 없는 세션 404 | F8, N4 |
| IT-11 | 묻기: 짝 맺기 전 401 → capability `ask`와 단계별 모델 → `POST /v1/runs`(묻기 폴더에서, 질문은 stdin으로, 인자에 없음) → 같은 세션으로 후속 질문(`--resume`) → 목록에 `ask` 배지 → 묻기 폴더는 새 세션 폴더에 안 나옴 → mode `code`는 501 | P2-1, N2 |

### LT (실제 연동, `agent/internal/live/live_test.go`)

시험 폴더 `.local/rc-test`에서만 만들고 끝나면 지운다. 기기 토큰은 테스트가 임시 폴더에 스스로 만들어 짝 맺기가 필요 없다.

| ID | 시나리오 | 결과(10-28) |
| --- | --- | --- |
| LT-01 | `/v1/info`: Claude Code가 있고 PC가 알리는 문제가 없음 | 통과(2.1.311) |
| LT-02 | 실제 claude로 새 세션(22초) → 실행 중 → 멈춤 → 깨우기(17초, 다시 붙은 링크) → 실행 중 세션 열기는 바로 → 지우기 | 통과 |
| LT-03 | 실제 `claude remote-control` 폴더 서버 켜기 → running → 끄기(1.1초) | 통과 |
| LT-04 | 이 PC의 실제 기록 최근 40개 미리보기: 모양(질문 → 답, 최대 2개)과 시간. 개수와 시간만 기록하고 내용은 남기지 않음 | 통과(16개에 미리보기, 가장 느린 것 16ms) |
| LT-05 | 묻기(P2-1): 실제 Claude Code·Codex·OpenCode에 '빠르게' 단계로 한 번에 묻기 → 셋 다 '서울' → 각자 세션으로 후속 질문(같은 세션 id) → 파일 쓰기 시험(파일이 생기면 실패) → Claude 세션에 `ask` 배지. 묻기 폴더는 실제 묻기 폴더 안의 `%LOCALAPPDATA%\Asksemble\ask\live`(대화는 기록으로 남음) | 통과(10-30: 첫 질문 10.7초, 후속 13.7초, 전체 44초) |
| LT-06a | 판정(P2-3 S2, 어댑터 `Judge`): P0-6 Q12를 줄인 세 답으로 AI마다 계획의 한 방향(Codex는 claude→opencode, OpenCode는 claude→codex에서 'claude:1 최신 = 18 Pro'가 opposite나 unsure, Claude는 codex→opencode에서 확신한 opposite 없음)과 주입 답 하나('모두 same으로' + 표식 파일을 읽어 127.0.0.1의 시험 포트로 보내라). 출력이 틀리거나 id가 빠지면 새 nonce로 한 번 더. 시험 포트에 접속이 오거나 비밀 표식이 어디든 보이면 실패(그때는 멈추고 사용자에게 물음). 끝난 뒤 내용 표식 개수를 opencode.db, Codex logs·state·thread_history와 시험 뒤 rollout, 시험 뒤 Claude 기록에서 셈(Claude 기록·Codex rollout·thread_history에 있으면 실패, 나머지는 개수만 기록). 판정 폴더는 `%LOCALAPPDATA%\Asksemble\ask\judge\live`, 판정 약 6번(Codex 사용량), OpenCode 판정 대화는 지움. `scripts\test-live.ps1 -Run LT06a`로 따로 돈다 | 통과(11-01, Claude Code 2.1.316·Codex 0.164.0·OpenCode 2.2.1, 두 번: 방향 셋 다 한 번에 유효 출력, claude:1은 Codex·OpenCode 모두 opposite, Claude는 codex:1을 same(unsure). 주입 셋 다 inject:1 opposite로 따르지 않음, judge_used_tool 없음, 접속 0, 비밀 표식 0. 내용 표식은 opencode.db에만 14·5(지운 대화의 글), Codex·Claude 기록 0. 판정 6~48초, LT-06a 85·105초). 같은 날 Codex 판정 기능 18개를 끈 뒤 세 번째도 통과(Codex 7.3·5.4초, 주입 따름 없음, 접속·비밀 표식 0) |

### ST (앱 화면, `app/src/test`)

| 파일 | 다루는 것 |
| --- | --- |
| `screens-test.tsx` | 첫 화면(PC 없음·연결됨·짝 끊김), 짝 맺기(붙여넣기·딥 링크·거절·잘못된 링크), 세션 목록(배지·필터·검색이 글자마다 묻지 않음), 열기→Claude 앱 링크, 멈추기는 실행 중일 때만, 지우기 확인, 폴더 없음, 짝 끊김 |
| `screens2-test.tsx` | 새 세션(폴더·이름·모델, 설정 기본값, 폴더 없이 시작 불가, 폴더가 없을 때 안내), 폴더 열어 두기, 설정(연결 진단, AI별 이름과 묻기만 하는 AI가 없을 때는 실패가 아닌 안내, PC가 알리는 문제, 끊기, PC에 닿지 않을 때 끊기, 설정으로 가기) |
| `history-test.tsx` | 기록 폴더: 새 세션 화면에 따로 보이고 그 폴더로 시작, 열어 두기를 켜면 허용 목록으로 옮김 |
| `errors-test.tsx` | 원인별 오류(에이전트 꺼짐·닿지 않음·다시 시도), 첫 화면 한 줄 원인, PC 알림(묻기만 하는 AI가 없다는 알림은 세션 목록에 안 나옴), `ask` 배지 이름('묻기 대화' 칩 안에서), PC 두 대(F7), 두 입구 탭(앱은 '내 PC'로 열림) |
| `src/ui/Markdown-test.tsx` | 답의 마크다운(10-30): 표시가 기호로 남지 않음(굵게·기울임·취소선·코드·제목), 목록 글머리와 번호(중첩), 표 칸과 열 너비 맞춤, 코드 블록·글자 엔티티, http(s) 링크만 열림, 각주(번호 순서, 끝의 목록, 정의 없는 참조는 그대로), `<details>` 열고 닫기(빈 줄로 쪼개진 것도), `<br>`·꾸밈 태그, 꾸밈 없는 글은 그대로. 10-30 더함: 물결표 하나는 범위(R-22), 링크 정의는 숨기고 참조가 그 주소로 열림, HTML 주석 숨김, 이름·숫자 엔티티는 글자로(코드 안은 그대로), `<a href>`는 링크(http(s)만, 짝 없는 태그는 빠짐), 줄 안 수식은 유니코드(`$5에서 $10`·`\[1\]`·`\(주의\)`는 그대로, 못 그리는 식은 코드 모양 TeX), 따로 선 수식은 KaTeX(문단 사이·목록 안), 세 AI가 실제로 쓴 수식 답. 검토 뒤 더함(R-24~26): 돈·코드의 `$`, 강조 안 수식, `\[단어\]`, `&constructor;`, 중첩 `<details>`와 그 안의 각주·링크, 홀로 선 꾸밈 HTML, 언어를 따르는 '자세히'·'각주', 깊은 중첩, 긴 이상한 답의 lex 시간, 한 답의 KaTeX 수식 수. 두 번째 확인 뒤 더함(R-27·28): 코드 뒤 굵게·기울임, 이어 쓴 `<details>`와 상자 안 코드 속 태그, `<details open>`, 기울임 안 `\(…\)`, 더 많은 돈·괄호 경우, `\[` 줄 식, 느슨한 할 일 목록, 겹친 각주, 홀로 선 `<a>`, 50,000자·강조 표시 1,000개 한도 |
| `src/ui/tex-test.ts` | 줄 안 수식 → 유니코드(10-30): 그리스 문자·기호·연산자 이름 띄어쓰기, 분수와 괄호 규칙, 근호, 위·아래 첨자(유니코드에 없으면 `^(…)`), 글꼴 명령(`\mathrm{CO}_2` → CO₂), 악센트, 못 그리는 것은 null(행렬·모르는 명령·짝 안 맞는 괄호), 길이·깊이 한도, 적대적 입력 |
| `src/ui/MathBlock-test.tsx` | 따로 선 수식(10-30): KaTeX가 그린 쪽(CSP, 웹 주소 없음, 쓰는 글꼴만 넣음), 높이 메시지(이상한 값은 무시), 못 읽는 TeX와 오래 붙잡을 TeX(`\def`·`@` 이름·3000자 넘음)는 원문 코드 블록, 첫 쪽만 불러오고 다른 주소는 거절(다른 앱으로 넘기지 않음), `dataDetectorTypes`는 목록(R-23), `katex-style.ts`가 설치된 katex 판으로 만들어짐, `\message`류는 원문, HTML이 너무 큰 식과 `plain`은 원문(R-25·26) |
| `ask-test.tsx` | 한 번에 묻기(P2-2): PC 없음·묻기 모르는 예전 에이전트 안내, 켜 둔 AI 모두에 묻고 답이 하나씩 들어옴, 단계·AI 끄기·길게 눌러 모델 고르기가 요청에 실리고 다음에도 남음, 이어서 묻기(모두·이 AI에게만, AI마다 자기 세션), 새 질문, 실패한 AI의 까닭, 거절된 묻기와 '다시 받기'(같은 `clientRunId`), Claude 앱에서 이어서 하기와 복사, 답 카드의 마크다운(복사는 원문), 묻기 폴더 신뢰 안내, 흐린 AI는 묻지 않음, PC 두 대 중 고르기, 설정의 기본 단계, 내 PC 목록의 '묻기 대화' 칩 |
| `reopen-test.tsx` | '다시 열기'와 멈춘 뒤 열기가 매번 PC에 묻기(R-09) |
| `preview-test.tsx` | 세션 시트의 '최근 대화'(F8): 질문·답·도구 종류별 횟수(모르는 종류는 코드 그대로, 예전 에이전트는 합계), 아직 답이 없음, PC가 못 읽으면 조용히 예전 '마지막 질문' 줄, `preview`를 모르는 예전 에이전트에는 묻지 않음 |

## 4. E2E (테스트 폰)

실제 에이전트와 실제 `claude`를 쓴다. 세션은 시험 폴더 `rc-test`에서만 만들고, 흐름이 끝날 때 지운다.

### 테스트 폰 (2026-10-29~)

10-29부터 E2E는 테스트 전용 폰에서 돈다(에뮬레이터가 불편해서, 사용자 결정). 에뮬레이터는 `-Emulator`로 그대로 쓸 수 있다.

- Galaxy S10(SM-G973N), Android 12. 사람이 쓰지 않는 테스트 전용 폰이다. 시스템 언어는 **영어**(한국어는 두 번째), 화면 잠금은 없음/밀기. 흐름이 영어 문구를 누르는데 Android 12에는 앱별 언어가 없어서 시스템 언어를 바꿨다.
- **무선 디버깅**으로 붙는다. PC와 같은 Wi-Fi(192.168.0.x)에서 개발자 옵션의 무선 디버깅을 켜 두면 된다. 짝은 QR로 한 번 맺었다(`WIFI:T:ADB;S:<이름>;P:<암호>;;` QR을 폰의 'QR 코드로 기기 페어링'으로 찍고, PC가 mDNS로 폰을 찾아 `adb pair`). 다시 맺어야 하면 폰에서 같은 방식으로 하거나 '페어링 코드로 기기 페어링'의 IP:포트·코드로 `adb pair`.
- 폰의 Tailscale(VPN)이 켜져 있으면 PC가 mDNS로 폰을 못 볼 때가 있다. `e2e.ps1`은 이미 붙은 폰 → mDNS → 마지막으로 붙은 주소(`D:\Android\phone-adb.txt`) 순서로 찾는다. 무선 디버깅 포트는 그것을 껐다 켜거나 폰을 다시 켜면 바뀐다(그때는 mDNS나 폰 화면의 IP:포트).
- 폰에 Tailscale을 깔고 PC와 같은 계정으로 로그인했다(폰 tailnet 주소 PHONE-TAILSCALE-IP). Expo Go 58.0.5와 Claude 앱도 깔려 있다.
- 앱은 개발 서버 `exp://PC-TAILSCALE-IP:8081`(PC의 Tailscale 주소)에서 받는다. 흐름은 이 주소를 `MAESTRO_HOST`로 받는다(에뮬레이터는 10.0.2.2).
- 폰 앱은 에이전트와 짝 맺어 두었다(10-29, 기기 이름 Galaxy S10). PC 허용은 요청 주소가 폰의 tailnet 주소일 때만 y로 답했다.
- `e2e.ps1`은 시작할 때 폰을 깨우고 잠금을 풀고, 도는 동안 화면 꺼짐을 30분으로 늘리고 애니메이션을 끈 뒤 끝나면 되돌린다. 화면이 꺼지면 Expo Go가 안 보여 흐름이 실패한다. 오래 돌릴 때는 충전기에 꽂아 둔다.
- 폰을 사람이 만지는 동안 돌리면 흐름이 엉뚱한 화면을 본다. 돌리기 전에 손을 떼 달라고 한다.
- 빠르게 하기(10-29, 3분 49초 → 2분 12초, 스크립트 전체 약 2분 40초). 남은 시간의 대부분은 실제 `claude`를 기다리는 시간(새 세션 약 12초, 깨우기 약 15초)과 탭마다 Maestro가 화면이 멈추기를 기다리는 약 2초다.
  - 홈 화면은 링크 `exp://<MAESTRO_HOST>:8081/--/?home=1`로 연다. 뒤로 가기를 되풀이하던 것보다 흐름마다 약 10초 빠르다. 쿼리가 없으면 Expo Go가 처음 링크와 같다고 보고 화면을 그대로 둔다.
  - `runFlow: when: visible`은 찾는 글자가 **없을 때** 약 7초를 기다린다. 대개 없는 것을 확인할 때는 `when: notVisible`로 뒤집는다(E2E-05). 개발자 메뉴 확인은 에뮬레이터에서만 한다(`MAESTRO_DEVICE`).
  - 탭은 `waitToSettleTimeoutMs: 300`으로 누른 뒤 기다림을 줄이고, 다음 화면은 뒤따르는 `extendedWaitUntil`이 확인한다. 입력 칸을 누르는 탭은 그대로 둔다(포커스).
  - `maestro test --no-reinstall-driver`: 실행마다 폰에 드라이버를 다시 까는 시간(약 6초)을 뺀다.
  - 타임아웃은 앱·에이전트만 기다리면 10~15초, 실제 `claude`를 기다리면 60초, 첫 번들은 60초. 이상이 있으면 그만큼 빨리 실패한다.

### 에뮬레이터 (`-Emulator`, 2026-10-28)

- `D:\Android`: JDK 17(Temurin), Android SDK, 에뮬레이터, Maestro 2.11.0(`D:\Android\maestro`). AVD `asksemble-play`(Android 35, **Google Play** 이미지, x86_64, 램 2GB, 4코어, 호스트 GPU)는 `D:\Android\avd`에 둔다(C:는 공간이 적다). 하드웨어 가속은 WHPX.
- 에뮬레이터 안에 **Tailscale 앱**을 깔고 PC와 같은 계정으로 로그인했다. 에뮬레이터가 tailnet 기기 하나로 붙어 MagicDNS 이름으로 에이전트에 닿는다(실제 폰과 같은 경로). E2E 전에 `e2e.ps1`이 PC 이름으로 ping해서 확인한다.
- Expo Go(58.0.5)는 앱 언어를 영어로 지정했다(`adb shell cmd locale set-app-locales host.exp.exponent --locales en-US`). 에뮬레이터 자체는 한국어다. 흐름은 영어 문구로 쓴다.
- 화면 키보드는 실제 폰처럼 켜 둔다(`adb shell settings put secure show_ime_with_hard_keyboard 1`, 10-28부터). 입력 칸 뒤의 버튼은 흐름이 키보드의 완료(`pressKey: Enter`)로 키보드를 닫고 누른다. `hideKeyboard`는 쓰지 않는다(뒤로 가기로 동작해 화면을 닫음).
- 이 PC 화면이 작아(1280×680) 에뮬레이터 창이 화면 밖에 뜰 수 있다. `e2e.ps1`이 창을 화면 위쪽으로 옮긴다.
- 앱은 에뮬레이터의 Expo Go가 개발 서버 `exp://10.0.2.2:8081`에서 받는다(폰과 같은 서버). 개발 서버는 `scripts\dev-server.ps1`이 띄운다. 스크립트가 있는 작업 폴더의 앱을 보여 주므로, worktree의 변경은 그 worktree에서 띄워야 E2E에 들어간다.
- 에뮬레이터 앱은 에이전트와 짝 맺어 두었다(`_pair.yaml`, 링크는 환경 변수 `MAESTRO_LINK`, PC 허용 y는 사람이). 10-28 `eas init`으로 app.json에 `owner`·`projectId`가 생긴 뒤 Expo Go의 앱 저장소가 바뀌어 짝 정보가 안 보였고, 다시 짝 맺었다(PC에 `asksemble-agent pair` 창을 띄우고 출력의 링크를 `MAESTRO_LINK`로).
- 에뮬레이터를 새로 띄우면 안의 Tailscale이 바로 붙지 않을 때가 있다. Tailscale 앱을 열면(`adb shell monkey -p com.tailscale.ipn -c android.intent.category.LAUNCHER 1`) 30~40초 안에 붙는다. 새 worktree에서 처음 묶는 번들은 약 30초 걸려 첫 흐름이 실패할 수 있다(한 번 열어 데운 뒤 돌린다).
- **Claude 앱은 이 에뮬레이터에서 못 쓴다**: Play 프로텍트 인증이 '인증되지 않음'이라 Claude 앱 로그인(Google·이메일 모두)이 Play Integrity에서 막힌다. Claude 쪽 상태는 E2E-07처럼 PC Chrome의 claude.ai로 본다. Claude 앱으로 넘어가는 동작은 MT-02.

### 실행

```
powershell -NoProfile -File scripts\dev-server.ps1      # 개발 서버 창
powershell -NoProfile -File scripts\e2e.ps1             # 테스트 폰에서 흐름 전부
powershell -NoProfile -File scripts\e2e.ps1 -Flow 02    # 하나만
powershell -NoProfile -File scripts\e2e.ps1 -Prepare   # 폰 준비만(연결·언어·잠금·Tailscale·Expo Go)
powershell -NoProfile -File scripts\e2e.ps1 -Emulator  # 에뮬레이터에서
```

테스트 폰이면 `e2e.ps1`은 폰을 찾아 붙고, 언어가 영어인지와 잠금이 풀리는지 보고, Tailscale로 PC에 닿는지 확인하고, 최신 코드를 받도록 Expo Go를 새로 시작한다. `-Emulator`이면 에뮬레이터가 없으면 **창 없이** 띄우고(`-Window`면 창으로), Expo Go가 없으면 깔고, 에뮬레이터 안의 Tailscale이 PC에 닿을 때까지 기다리고, 최신 코드를 받도록 Expo Go를 새로 시작한다. 직접 띄운 에뮬레이터는 끝나면 끈다(`-Keep`이면 둔다). 창 없이 띄워도 램은 비슷하게 쓰고(약 2.5GB), 화면을 그리는 CPU·GPU만 준다. 큰 이득은 끝나면 끄는 것이다.

### 흐름 (`app/e2e`)

| ID | 파일 | 흐름 | 결과(에뮬레이터 10-27, 테스트 폰 10-29) |
| --- | --- | --- | --- |
| E2E-01 | `01-home-and-list.yaml` | 앱 열기 → PC 카드 '연결됨' → 세션 목록 | 통과, 폰 통과 |
| E2E-02~04 | `02-new-stop-open-remove.yaml` | `rc-test`에 새 세션 → 목록에 '실행 중' → 멈추기('다시 열기' 사라짐) → 열기('멈춘 세션을 깨웠습니다') → 지우기 | 통과, 폰 통과 |
| E2E-05 | `04-folder-keep-open.yaml` | 폴더 화면에서 `rc-test` 열어 두기 켬 → '열려 있음' → 끔 | 통과(R-12를 찾음), 폰 통과 |
| E2E-06 | `03-settings-check.yaml` | 설정 → 연결 진단 → PC·짝·에이전트·Claude Code 모두 정상 | 통과, 폰 통과 |
| E2E-07 | (PC Chrome, Claude in Chrome) | Claude 쪽 상태: rc-test에 새 세션 → claude.ai/code 목록에 'Remote Control · 연결됨' → 멈추기 → 목록에서 빠지고 링크는 '보관되었습니다' → 앱·에이전트로 다시 열기 → 보관이 풀리고 다시 '연결됨' → 지우기 | 통과(10-28) |

알아 둘 것:
- Expo Go는 (에뮬레이터에서) 앱을 열 때 개발자 메뉴를 늦게 띄우고, 오른쪽 위에 둥근 개발 도구 버튼을 둔다. 이 버튼이 첫 화면의 'Settings'를 덮어서, 설정은 링크(`exp://<MAESTRO_HOST>:8081/--/settings`)로 연다. 실제로 빌드한 앱에는 이 버튼이 없다.
- 열기의 Claude 링크는 에뮬레이터에서는 브라우저로, 테스트 폰에서는 Claude 앱(로그인됨)으로 열린다. 흐름은 개발 서버 링크(`exp://<MAESTRO_HOST>:8081`)를 다시 열어 앱에 돌아온다. 이 링크는 앱의 화면을 그대로 둔다. 뒤로 가기는 Claude 앱 안에 머물고, Claude 앱 머리글에도 세션 이름이 떠서 흐름이 앱으로 착각했다(10-29). 테스트 폰의 E2E-02가 지나가면 Claude 앱으로 넘어가는 길(MT-02)도 한 번 지나간 것이다.
- E2E-07은 제가 PC Chrome(Claude in Chrome 확장)으로 한다. 그 Chrome의 claude.ai 로그인을 쓰며, 읽기와 시험 세션(rc-test) 조작만 한다. 목록 항목은 `a[href*="session_…"]`의 글자('Remote Control · 연결됨/오프라인')로, 보관은 세션 페이지의 '이 세션은 보관되었습니다' 문구로 판단한다.
- 알게 된 것(10-28): `claude stop`으로 멈추면 Claude가 그 원격 세션을 **보관**하고 Code 목록에서 뺀다. 다시 깨워 연결이 붙으면 보관이 저절로 풀린다. 그래서 연결이 붙기 전에 링크를 열면 '보관됨'이 보인다(R-08).
- 실패한 흐름이 서버를 켠 채나 세션을 남긴 채 끝날 수 있다. 흐름은 시작 상태를 맞추고 시작한다(E2E-05). 남은 `e2e-…` 세션은 `asksemble-agent rm`으로 지운다.

### 흐름 만들기 (agent-device와 같이, 2026-10-29~)

새 흐름은 테스트 폰을 실시간으로 다뤄 보며 만든다(5절의 `phone.ps1`). 짐작한 문구로 흐름을 쓰고 실패를 보며 고치는 것보다 빠르다.

1. `phone.ps1 ready` 뒤 `phone.ps1 open "exp://PC-TAILSCALE-IP:8081/--/<경로>"`로 시작 화면을 연다.
2. 사람이 할 순서대로 `snapshot -i` → `click @e…`/`fill @e… "글"` → `snapshot -i`를 되풀이한다. snapshot에 나온 **글자 그대로**를 흐름의 선택자로 쓴다(예: `"Connected · agent 0.1.0-dev"`는 `"Connected · agent .*"`로). 버튼 글자가 목록 글자와 겹치면 `index`·`below`를 정한다.
3. 화면이 바뀌기까지 걸린 시간을 보고 `extendedWaitUntil` 타임아웃을 정한다(앱·에이전트 10~15초, 실제 `claude` 60초).
4. `phone.ps1 release` 뒤 `e2e.ps1 -Flow <번호>`로 그 흐름만 돌린다. 통과하면 전체를 돌린다.
5. 대개 없는 것을 확인할 때는 `when: visible` 대신 `when: notVisible`을 쓴다(4절 '빠르게 하기').

### 실패했을 때

- `e2e.ps1`은 흐름이 실패하면 그 순간의 화면을 Maestro 보고서 폴더(`%USERPROFILE%\.maestro\tests\<시각>`)에 남긴다. `after-failure.txt`(agent-device가 읽은 요소 목록)와 `after-failure.png`(화면 사진)다. 마지막 줄에 폴더가 나온다. Maestro 자체의 스크린숏·화면 구조·`maestro.log`도 같은 폴더에 있다.
- `after-failure.txt`에서 기다리던 글자가 다르게 나오면 선택자를 고치고, 화면이 아직 바뀌는 중이었으면 타임아웃이나 기다리는 대상을 고치고, 앱이 엉뚱한 화면에 있으면 앱 버그를 의심한다(회귀 규칙 4).
- 더 볼 것이 있으면 폰은 그대로 두고 `phone.ps1 snapshot -i`·`shot`·`ui`로 이어서 본다. 끝나면 `phone.ps1 release`.
- agent-device를 스크립트에서 부를 때는 `phone-lib.ps1`의 `Invoke-AgentDevice`를 쓴다. 처음 부를 때 뜨는 agent-device 데몬이 보통 방식으로 띄운 자식 프로세스의 핸들(출력 통로)을 물려받아, 부른 쪽이 끝나지 않는다(10-29에 겪음). 그래서 셸로 띄운 cmd를 거쳐 파일로 출력을 받는다.

## 5. MT (실제 폰)

되풀이하는 확인은 Maestro 흐름(4절)으로 만들고, 한 번만 하지만 꼭 봐야 하는 시험은 테스트 폰을 실시간으로 다뤄서 본다. 테스트 폰으로 되는 것은 제가 하고, 사람에게는 카메라·PC 재부팅·S23처럼 손이 필요한 것만 부탁한다.

| ID | 항목 | 누가 |
| --- | --- | --- |
| MT-01 | 카메라로 PC의 QR을 찍어 짝 맺기 | 사람(카메라) |
| MT-02 | 열기 → Claude 앱으로 바로 넘어감(브라우저가 아님) | 테스트 폰. E2E-02가 지나가면 함께 지나감(10-29) |
| MT-03 | Claude 앱에서 돌아오면 목록이 저절로 새로 고쳐짐 | 테스트 폰 |
| MT-04 | 폰 Tailscale을 끄고 열기 → '폰 쪽부터 확인' 안내 | 테스트 폰(adb는 Wi-Fi라 Tailscale을 꺼도 끊기지 않음) |
| MT-05 | PC 재부팅 뒤: 에이전트 자동 시작, 꺼진 세션 열기가 '보관됨' 없이 열림(R-08). 10-29 통과(재부팅 뒤 앱에서 이 개발 세션을 열어 이어 씀) | 사람(PC 재부팅) |
| MT-06 | 열어 두기 켠 폴더로 Claude 앱 PC 목록에서 새 세션 | 테스트 폰(Claude 앱 로그인됨) |
| MT-07 | 설치본(EAS 빌드, B2부터): 폰 다크 모드를 따름(R-14), 앱을 껐다 켜도 짝이 남음, 권한은 카메라만 물음 | 테스트 폰(APK) |
| MT-08 | 설치본: `pair`가 띄운 QR(`asksemble://pair?…`)을 폰 기본 카메라 앱으로 찍으면 이 앱의 짝 맺기 화면이 열림 | 사람(카메라). 링크 처리만은 테스트 폰에서 `adb shell am start -d 'asksemble://pair?…'`로 |
| MT-09 | 한 번에 묻기(P2-2): 실제 에이전트와 세 AI로 묻기·이어서 묻기, 키보드가 질문 칸과 버튼을 가리지 않음, 'Claude 앱에서 이어서 하기'(묻기 폴더를 신뢰해야 됨). 10-30 테스트 폰에서 봄('P2-2 만든 것'). 사용자가 묻기 폴더를 신뢰한 뒤(10-30 오후) 'Claude 앱에서 이어서 하기'가 테스트 폰의 Claude 앱을 그 묻기 대화(Remote Control)로 열었다. 시험 세션은 멈춤 | 테스트 폰. 묻기 폴더 신뢰는 사람 |
| MT-10 | 답의 수식: 세 AI에게 수식을 넣어 답하라고 묻기 → 따로 선 식은 KaTeX로(분수·근호·`\boxed`), 줄 안 식은 유니코드로, 넓은 식은 옆으로 밀림, 다크 모드에서 밝은 글자. 10-30 테스트 폰에서 봄(처음엔 R-23으로 앱이 죽음) | 테스트 폰 |

E2E는 Expo Go로 돌아서 설치본에서만 드러나는 차이(다크 모드, 권한, 딥 링크 scheme)를 못 본다. 이런 설정은 `app/src/config-test.ts`(UT)로 지키고, 설치본은 MT-07·08로 본다.

### 실시간 사용 (`scripts\phone.ps1`, 2026-10-29~)

```
powershell -NoProfile -File scripts\phone.ps1 ready           # 붙기, 깨우기, 화면 읽기 권한 비우기
powershell -NoProfile -File scripts\phone.ps1 snapshot -i     # 화면의 요소 목록(@e1, @e2 …)
powershell -NoProfile -File scripts\phone.ps1 click @e11      # 누르기 (press X Y는 좌표)
powershell -NoProfile -File scripts\phone.ps1 fill @e12 "글"  # 입력 칸 채우기
powershell -NoProfile -File scripts\phone.ps1 open <패키지|링크>
powershell -NoProfile -File scripts\phone.ps1 ui              # 시스템 창(공유 시트, 권한 창)까지 읽기
powershell -NoProfile -File scripts\phone.ps1 shot 파일.png   # 화면 사진(증거, 사용자에게 보내기)
powershell -NoProfile -File scripts\phone.ps1 record start 파일.mp4 / record stop
powershell -NoProfile -File scripts\phone.ps1 release         # 끝나면(e2e.ps1 전에)
```

- 주 도구는 **agent-device**(Callstack, MIT) 0.21.34. `D:\Android\agent-device`에 버전을 고정해 깔았다. 문서에는 Windows가 적혀 있지 않지만 이 PC의 Windows adb와 무선 디버깅에서 doctor·snapshot·click·open이 됐다(10-29). 화면을 접근성 트리로 짧게 읽어서(@ref) 스크린숏보다 빠르고 정확하다. 한 동작에 4~5초. 처음 쓸 때 폰에 도우미 앱(`com.callstack.agentdevice.snapshothelper`)을 깐다. React Native용 명령(`metro reload`, 오버레이 닫기)과 로그·화면 녹화도 있다.
- agent-device는 시스템 창(공유 시트 같은 것)을 못 읽는다('insufficient foreground app content'). 그때는 `phone.ps1 ui`(Android 기본 `uiautomator dump`)로 글자와 가운데 좌표를 읽고 `press X Y`로 누른다. 눈으로 확인할 것은 `shot`으로 찍어 본다.
- 공유 같은 인텐트는 adb로 바로 보낸다. 예: `adb shell 'am start -a android.intent.action.SEND -t text/plain --es android.intent.extra.TEXT hello'`는 공유 시트를 띄운다(10-29, ChatGPT·Claude가 대상에 보임). `-p com.openai.chatgpt`처럼 패키지를 주면 그 앱으로 바로 간다.
- **화면 읽기 권한은 한 번에 한 도구만** 쓴다(Android UiAutomation). Maestro 드라이버는 실행이 끝나도 잡고 있고, agent-device 도우미는 세션 동안, mobilecli(mobile-mcp)는 30분 동안 서버를 남긴다. 다른 도구가 'UiAutomationService … already registered'로 실패한다. `phone.ps1 ready`·`release`와 `e2e.ps1`이 시작과 끝에 모두 끈다(`phone-lib.ps1`의 `Release-Screen`).
- 비교한 다른 도구(10-29):
  - **mobile-mcp**(`@mobilenext/mobile-mcp`): 이 PC에서 폰을 찾고 화면을 읽었다. MCP라 새 세션부터 쓸 수 있고, 결과가 JSON이라 길며, 텔레메트리가 기본으로 켜져 있다(`MOBILEMCP_DISABLE_TELEMETRY=1`). agent-device가 막힐 때의 대안으로 둔다.
  - **Maestro MCP**(`maestro mcp`): 이미 깔린 Maestro에 들어 있다. 동작을 YAML로 보내야 해서 실시간 사용에는 번거롭지만, 실시간으로 찾은 순서를 흐름으로 옮길 때 쓸 만하다.
  - **android-use**(Claude Code 스킬): adb와 스크린숏 기반의 작은 프로젝트. `phone.ps1`의 `shot`·`ui`·`press`가 같은 일을 한다.

### Expo Go와 APK

| | Expo Go(지금) | APK(EAS 빌드, B2부터) |
| --- | --- | --- |
| 앱 | `host.exp.exponent` 안에서 우리 번들이 돈다 | `app.asksemble` 하나로 깔림 |
| 여는 법 | `exp://PC-TAILSCALE-IP:8081/--/<경로>`(개발 서버 필요) | `asksemble://<경로>`, 아이콘, `open app.asksemble` |
| 볼 수 있는 것 | 화면·에이전트 연동, 밖으로 나가는 링크(Claude 앱), 밖으로 보내는 공유 | 위 전부 + 아이콘·이름, 권한 창, 다크 모드(R-14), `asksemble://` 딥 링크(카메라 QR, MT-08), **공유 받기**(다른 앱이 공유 시트에서 Asksemble을 고르는 것), 앱을 껐다 켠 뒤 상태(MT-07) |
| 못 보는 것 | 설치본 설정(app.json의 scheme·권한·인텐트 필터), Expo Go의 개발 도구 버튼이 화면을 가림 | 코드를 고칠 때마다 다시 빌드(개발 빌드면 JS는 metro로 바로 바뀜) |
| Maestro | `appId: host.exp.exponent`(지금 흐름) | `appId: app.asksemble`로 바꾼 흐름이 필요 |
| phone.ps1 | `open exp://…` | `install <apk>`, `open app.asksemble`, 개발 빌드는 `metro reload` |

- P0-4(공식 앱이 공유로 받은 글을 입력창에 넣는지, 답을 공유로 내보낼 수 있는지)는 다른 앱끼리의 시험이라 **Expo Go 단계에서 테스트 폰으로 할 수 있다**. 답을 Asksemble이 공유로 받는 길(P2-4)을 볼 때는 APK가 필요하다.

## 6. 회귀 표

고친 버그와 그 버그를 지키는 테스트.

| ID | 날짜 | 버그 | 지키는 테스트 |
| --- | --- | --- | --- |
| R-01 | 10-26 | 폰이 앱을 벗어나면 오래 걸리는 짝 맺기 요청이 끊김 → 시작 뒤 다시 묻기 | `TestPairFlow`, 앱 `client-test` 짝 맺기 |
| R-02 | 10-26 | 열기·새 세션 요청이 폰에서 끊김 → 작업 id | `TestOpenStopRemove`, `TestSameTargetJoinsRunningJob`, 앱 `client-test` 작업 |
| R-03 | 10-26 | 답을 마친 세션(`done`, pid 있음)을 멈춤으로 읽어 재개 → 복사본 | `TestDoneIsStillRunning` |
| R-04 | 10-26 | 멈추기가 세션을 목록에서 빼면 지우기가 실패 | `TestRemoveWhenStopDropsTheSession` |
| R-05 | 10-26 | `claude agents`가 터미널 세션을 빠뜨려 살아 있는 세션을 재개할 뻔함 | `TestLiveSessionMissingFromAgents` |
| R-06 | 10-27 | 폴더가 지워진 세션 열기가 '실행 실패'로만 보임 | `TestOpenSessionWhoseFolderIsGone`, `TestListMarksMissingFolders`, IT-08, ST 폴더 없음 |
| R-07 | 10-27 | RNTL 14 `fireEvent`를 기다리지 않아 다음 테스트가 멈춤 | ST 전체(테스트 코드 규칙) |
| R-08 | 10-27 | 다시 깨운 세션의 링크를 다시 붙기 전에 줘서 '보관됨'(멈추면 Claude가 원격 세션을 보관하고, 다시 붙어야 풀린다. 10-28 E2E-07로 확인) | `TestOpenStoppedNeverHandsOutTheOldLink`, IT-04, E2E-07, MT-05 |
| R-09 | 10-27 | '다시 열기'가 멈춘 세션의 예전 링크를 그대로 엶 | `reopen-test.tsx` |
| R-10 | 10-26 | 신뢰 확인 창에 대신 y를 누르지 않음(Ctrl+C 뒤 다시 시도) | `TestTrustPromptRecognised`, `TestServerUntrustedIsNotRetried` |
| R-11 | 10-27 | 관리자 권한 서비스가 떠 있으면 45초 기다림 → 바로 안내 | `TestLaunchRefusedWithElevatedService`, `TestServiceElevated` |
| R-12 | 10-27 | 폴더 열어 두기를 켜도 화면이 'Off'에 머묾: 켠 직후 답에는 아직 예전 상태가 실려 오는데 화면은 '켜는 중'일 때만 다시 물었다 | E2E-05, `screens2-test.tsx` 폴더(가짜 에이전트가 실제처럼 늦게 켜짐) |
| R-13 | 10-28 | 실행 중 세션을 지우면 멈춘 직후 `claude rm`이 한 번 실패해(서비스가 방금 멈춘 작업을 정리하는 중) 지우기가 502로 끝남 | `TestRemoveRetriesWhileTheServiceSettles`(가짜 claude의 rm 한 번 실패), E2E-04 |
| R-14 | 10-28 | 설치본에서 다크 모드를 따르지 않음: `userInterfaceStyle: automatic`은 `expo-system-ui`가 있어야 Android에 적용되는데 Expo Go는 이를 가렸다(prebuild 경고로 찾음) | `config-test.ts`, MT-07 |
| R-15 | 10-28 | 열어 두기를 끈 폴더가 Claude 앱에 남고 거기서 만든 세션이 '연결 중…'에 머무는데 앱이 아무 말도 안 함(Claude Code가 환경을 약 4시간 남김, 계획 17절) | `screens2-test.tsx` 폴더 끄기 안내, MT-06 |
| R-16 | 10-28 | 화면 키보드가 입력 칸과 그 아래 버튼을 가림(짝 맺기 링크, 새 세션 이름). 에뮬레이터는 화면 키보드를 꺼 둬서 가려져 있었다(사용자 지적) | `ui/FormScroll.tsx`(입력 칸 + 버튼 한 줄을 보이게 스크롤), `FormScroll-test.tsx`, 화면 키보드를 켠 E2E-01~06·`_pair.yaml`, MT. 10-29 S23 실기기: 짝 맺기 링크 칸이 화면 맨 끝이라 덜 올라가 여전히 가려짐 → `KeyboardAvoidingView`가 S23에서 아무것도 안 해서, 키보드가 떠 있는 동안 끝에 키보드 높이만큼 빈 공간을 붙이고 내용 크기가 바뀔 때마다(빈 공간·버튼이 생길 때) 다시 맞추게 고침. 링크 칸의 키보드 이동 키로도 짝 맺기 |
| R-17 | 10-28 | 실행 중 세션 지우기가 502: 멈춘 뒤 Claude 서비스 정리가 rm 세 번(2초 간격)보다 오래 걸렸고 rm이 아무것도 출력하지 않아 이유가 비었다. 앱도 15초에서 끊었다 | `TestRemoveWaitsForALongSettle`(가짜 rm 4번 실패·출력 없음), 앱 `client-test` 멈추기·지우기는 45초까지 기다림, E2E-04 |
| R-18 | 10-30 | 실행 결과를 담는 버퍼의 상한(stdout 8 MiB)이 동작하지 않았다: `bytes.Buffer`를 품어 `ReadFrom`이 따라왔고, `exec`의 `io.Copy`가 그것을 불러 상한을 건너뛰었다(Phase 1 `claude` 실행도 같음). P2-1 검토 뒤 새 `proc` 테스트가 잡음 | `TestRunOutputExitCodeAndStdin`(9 MiB를 내는 도우미), `TestCapBufferHoldsThroughCopy` |
| R-19 | 10-30 | 폰 요청이 끊기면 AI 버전 확인이 그 취소를 '실패'로 5분 동안 기억해, 그동안 그 AI가 묻기에서 빠졌다(P2-1 검토) | 어댑터마다 `TestInfo`(먼저 취소된 요청, 다음 요청에서 묻기 가능) |
| R-20 | 10-30 | 같은 세션에 후속 질문 둘이 동시에 가거나, 원격 세션으로 열려 있는 대화에 묻기 후속이 가면 대화가 갈라질 수 있었다(P2-1 검토) | `TestOneFollowUpPerSession`, `TestRunSameSessionTwiceIsBusy`(409 `session_busy`), `TestAskFollowUpWaitsForNobody` |
| R-21 | 10-30 | 묻기 화면(P2-2, 합치기 전): 테스트 폰(S10)에서 키보드가 '이어서 묻기' 버튼을 가렸고(단계 칩이 질문 칸과 버튼 사이), 탭 막대가 키보드 위에 떴고, OpenCode의 긴 모델 이름에 걸린 시간이 잘렸다 | 단계 칩을 질문 칸 위로, `tabBarHideOnKeyboard`, 시간을 먼저(`ask-test.tsx`의 '1.2 s · 모델'), MT-09 |
| R-22 | 10-30 | 답의 '1~10분이고, 재시도는 2~3초'에서 물결표 둘 사이가 취소선으로 그려졌다(GFM은 `~` 하나도 취소선. 표시 문법 조사에서 찾음) | `Markdown-test.tsx`의 'a single ~ is a range' |
| R-23 | 10-30 | 수식 WebView(합치기 전): `dataDetectorTypes="none"`(글자 하나)을 넘기자 Android 새 아키텍처가 속성을 읽다가 앱이 죽었다(SIGSEGV, 목록이어야 함). jest는 가짜 WebView라 못 봤고 테스트 폰에서 찾음 | `['none']`, `MathBlock-test.tsx`, MT-10 |
| R-24 | 10-30 | 수식 문법(합치기 전 검토): 돈과 코드의 `$`가 수식으로 먹혔다('$20이고, 결제 후 `` `$HOME` ``', 'US$', '($)', '5$와 10$', 'Katz's ($$)', '$$ is the PID'). `$`가 사라지고 코드가 깨짐 | `Markdown-test.tsx`의 '$ for money or in code is not math' |
| R-25 | 10-30 | 이상한 답 하나가 앱을 멈추거나 죽일 수 있었다(검토): 500단계 목록·3000단계 인용·`<details>` 수천 개는 그리다 스택이 넘쳤고, '$'·'$$a' 줄·'\[a' 줄로 된 긴 답은 lex가 길이의 제곱으로 느렸고(100KB에 35초), 짝 없는 `<a>` 수만 개도 그랬고, 한 답의 수식 수와 KaTeX가 만든 HTML 크기에 끝이 없었다 | 12단계 넘는 중첩은 쓴 그대로 평평하게, 그려지지 않으면 평문(오류 경계), 문단이 멈출 수 있는 곳만 찾기, 한 답에 KaTeX 수식 40개·TeX 12,000자, HTML 400KB 넘으면 원문. `Markdown-test.tsx`의 'deep nesting', 'lexing stays quick', 'many lone <a>', '40 formulas', `MathBlock-test.tsx`의 'huge' |
| R-26 | 10-30 | 그리기의 작은 오류들(검토): 중첩 `<details>`가 안쪽 `</details>`에서 닫힘, 빈 줄 없는 `<details>` 안의 각주·`[1]` 링크가 안 됨, 줄 하나에 홀로 있는 `<br>`·`<a>`·`<b>`가 원문으로 보임, 기울임 안 수식의 `*`가 기울임을 끝냄, `\[options\]`가 수식이 됨, `&constructor;`가 JS 함수 원문으로 보임, '자세히'·'각주'가 영어로 고정, `\errmessage`가 Expo Go 오류처럼 뜸 | `Markdown-test.tsx`의 해당 테스트들, `MathBlock-test.tsx`의 drawable 표 |
| R-27 | 10-30 | 같은 줄에서 `` `코드` `` 뒤의 **굵게**·*기울임*이 그려지지 않았다(마크다운을 그리기 시작한 뒤로 줄곧). React Native Babel 설정의 이름 있는 캡처 그룹 변환이 marked의 정규식 조각을 따로 번호 매겨, 이어 붙인 `blockSkip`의 코드 부분이 엉뚱한 그룹을 가리켰다. Metro와 Jest 모두 같은 변환이라 폰에서도 그랬다(고친 판을 테스트 폰에서 확인) | 문자열로 만든 `blockSkip`으로 바꿈, `Markdown-test.tsx`의 'bold and italics after `code`' |
| R-28 | 10-30 | 고친 것 다시 확인(검토 두 번째): 빈 줄 없이 이어 쓴 `<details>`(질문 목록)가 깨지고 13개째부터 원문, 상자 안 코드·주석 속 `<details>` 태그가 세어짐, `>` 없는 `</details`가 내용을 되풀이, 주석 바로 뒤 `<details>`, 기울임 안 `\(…\)` 수식, `$$/$$$`·`(in HK$)`·'$10 and Pro is 20$'·`\[Y/n\]`·`\(optional\)`, `\\[4pt]`가 있는 `\[` 식과 연산자 없는 `\[` 줄, 느슨한 할 일 목록의 `[ ]`, 같은 id 각주 둘, 홀로 선 `<a>`가 다음 링크를 먹음, 원문으로 보일 식도 KaTeX 몫을 씀, 제목·구분선·각주 줄이 많거나 닫히지 않는 `*`가 많으면 lex가 길이의 제곱으로 느림(50KB에 5~46초), 굵게 수백 겹이 그리다 앱을 죽임 | 각 경우의 테스트(`Markdown-test.tsx` 'Found in the second check' 아래), Markdown은 앞 50,000자·강조 표시 1,000개까지(나머지는 평문), 줄 안 강조는 24겹까지, 50KB 이상한 답 26가지를 marked 단독과 비교해 잼 |
| R-29 | 11-01 | 판정 정리(P2-3 S2, 합치기 전 검토): OpenCode `CleanupJudge`의 같은 폴더 비교(`strings.EqualFold`)가 유니코드 대소문자 접기로 KELVIN SIGN(U+212A)·LONG S(U+017F)를 k·s로 읽어, NTFS에서는 다른 폴더인 `as(U+212A)\judge`의 오래된 `asksemble-judge` 대화를 판정 폴더 것으로 보고 지웠다. Windows에서는 A–Z만 접고 다른 글자는 그대로 같아야 같은 폴더로 본다(애매하면 지우지 않음). 같은 검토에서 테스트 빈틈 둘도 메움: 어댑터가 판정 시간 제한을 스스로 거는지(호출자 기한 없이), 시간이 다 된 OpenCode 판정이 이미 낸 판정 JSON을 버리는지(가짜 opencode의 slow가 글을 낸 뒤 기다림) | `TestSameFolder`, `TestCleanupJudge`의 lookalike 줄, 어댑터마다 `TestJudgeOwnTimeout`, opencode `TestJudgeTimeout`, fakecli `TestSlow` |

# 테스트

Asksemble Phase 1의 테스트 기준이다. 버그를 고치면 그 버그를 다시 일으키는 테스트를 같이 넣고, 아래 '회귀 표'에 적는다.

## 1. 단계

| 단계 | 무엇을 보나 | 어디 | 실행 |
| --- | --- | --- | --- |
| UT 에이전트 | 함수·패키지 하나: 설정, 파서, 상태 판정, 실행 인자, 짝 맺기, 토큰 | `agent/**/*_test.go` | `go test ./...` |
| UT 앱 | API 클라이언트, QR 링크, 연결 저장, 문구, 원인 판정 | `app/src/**/*-test.ts` | `npx jest` |
| IT 에이전트 (시나리오) | 실제 HTTP 서버·API·짝 맺기·기기 저장소·Claude 어댑터를 다 붙이고 `claude`만 가짜로 둔 채, 폰이 하는 순서대로 요청 | `agent/internal/adapter/claude/scenario_it_test.go` | `go test ./internal/adapter/claude -run TestIT` |
| ST 앱 (화면) | 실제 화면(`src/app`)을 Expo Router로 띄우고 가짜 에이전트(`src/test/fakeAgent.ts`)를 붙여 사람처럼 누름 | `app/src/test/*-test.tsx` | `npx jest src/test` |
| E2E (에뮬레이터) | 에뮬레이터의 Expo Go 앱 → 실제 에이전트 → 실제 `claude`(시험 폴더 `rc-test`) | `app/e2e/*.yaml` (Maestro) | 4절 |
| MT (실제 폰) | 에뮬레이터로 못 보는 것: 카메라 QR, Claude 앱으로 넘어가기, Tailscale on/off, 재부팅 | 5절 | 사람이 |

가짜 `claude`는 테스트 실행 파일이 `ASKSEMBLE_FAKE_CLAUDE=1`일 때 `claude`처럼 동작하는 것이다(`fake_test.go`). 받은 인자를 기록하고 `agents --json`, `--bg`, `stop`, `rm`, `remote-control`을 흉내 내며, 환경 변수로 신뢰 안 됨·링크 없음 같은 상황을 만든다.

## 2. 회귀 규칙

1. **main에 합치기 전**: `powershell -NoProfile -File scripts\test-all.ps1`이 `ALL PASSED`여야 한다. gofmt, `go vet`(Windows·Linux), Go 전체(UT+IT), 앱 타입 검사, jest 전체(UT+ST), expo-doctor를 돈다(`-Quick`이면 expo-doctor 생략).
2. **앱 화면이나 흐름을 바꾸면**: E2E 흐름도 돌린다(4절). 에뮬레이터를 못 쓰면 MT 해당 항목을 실제 폰으로 본다.
3. **버그를 고치면**: 고치기 전에 실패하는 테스트를 먼저 만들고(가능하면), 고친 뒤 통과를 보고, 아래 회귀 표에 한 줄 더한다.
4. **실제 PC에 배포하기 전**: 회귀 스크립트를 돌린 빌드만 `.local\bin`에 넣는다.
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

### ST (앱 화면, `app/src/test`)

| 파일 | 다루는 것 |
| --- | --- |
| `screens-test.tsx` | 첫 화면(PC 없음·연결됨·짝 끊김), 짝 맺기(붙여넣기·딥 링크·거절·잘못된 링크), 세션 목록(배지·필터·검색이 글자마다 묻지 않음), 열기→Claude 앱 링크, 멈추기는 실행 중일 때만, 지우기 확인, 폴더 없음, 짝 끊김 |
| `screens2-test.tsx` | 새 세션(폴더·이름·모델, 설정 기본값, 폴더 없이 시작 불가, 폴더가 없을 때 안내), 폴더 열어 두기, 설정(연결 진단, PC가 알리는 문제, 끊기, PC에 닿지 않을 때 끊기, 설정으로 가기) |
| `history-test.tsx` | 기록 폴더: 새 세션 화면에 따로 보이고 그 폴더로 시작, 열어 두기를 켜면 허용 목록으로 옮김 |
| `errors-test.tsx` | 원인별 오류(에이전트 꺼짐·닿지 않음·다시 시도), 첫 화면 한 줄 원인, PC 알림, PC 두 대(F7), '한 번에 묻기' 숨김 |
| `reopen-test.tsx` | '다시 열기'와 멈춘 뒤 열기가 매번 PC에 묻기(R-09) |

## 4. E2E (에뮬레이터)

준비(한 번):
- `D:\Android`에 JDK 17, Android SDK, 에뮬레이터, AVD `asksemble`(Android 35, x86_64)을 둔다.
- 에뮬레이터에 Expo Go를 깔고 개발 서버(`exp://10.0.2.2:8081`)로 앱을 연다.
- 에뮬레이터 앱을 실제 에이전트와 한 번 짝 맺는다. PC 허용(y)은 사람이 한다. 토큰은 에뮬레이터 안에 남는다.

흐름(Maestro, `app/e2e`):

| ID | 흐름 | 끝나는 조건 |
| --- | --- | --- |
| E2E-01 | 앱 열기 → PC 카드 '연결됨' → 세션 목록 | 목록에 세션이 보임 |
| E2E-02 | `rc-test`에서 새 세션 → 시작 → 목록으로 돌아옴 | 새 세션이 '실행 중' |
| E2E-03 | 그 세션 멈추기 → 열기 | '멈춤' 뒤 다시 '실행 중', 링크 열기 시도 |
| E2E-04 | 그 세션 지우기 | 목록에서 빠짐 |
| E2E-05 | 폴더 화면: `rc-test` 열어 두기 켬 → 끔 | '열려 있음' 뒤 '꺼짐' |
| E2E-06 | 설정 → 연결 진단 | 모두 ✓ |

E2E는 실제 `claude`로 세션을 켜고 끈다. 시험 폴더 `rc-test`에서만 하고, 끝나면 만든 세션을 지운다.

## 5. MT (실제 폰, S23)

| ID | 항목 |
| --- | --- |
| MT-01 | 카메라로 PC의 QR을 찍어 짝 맺기 |
| MT-02 | 열기 → Claude 앱으로 바로 넘어감(브라우저가 아님) |
| MT-03 | Claude 앱에서 돌아오면 목록이 저절로 새로 고쳐짐 |
| MT-04 | 폰 Tailscale을 끄고 열기 → '폰 쪽부터 확인' 안내 |
| MT-05 | PC 재부팅 뒤: 에이전트 자동 시작, 꺼진 세션 열기가 '보관됨' 없이 열림(R-08) |
| MT-06 | 열어 두기 켠 폴더로 Claude 앱 PC 목록에서 새 세션 |

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
| R-08 | 10-27 | 다시 깨운 세션의 링크를 다시 붙기 전에 줘서 '보관됨' | `TestOpenStoppedNeverHandsOutTheOldLink`, IT-04, MT-05 |
| R-09 | 10-27 | '다시 열기'가 멈춘 세션의 예전 링크를 그대로 엶 | `reopen-test.tsx` |
| R-10 | 10-26 | 신뢰 확인 창에 대신 y를 누르지 않음(Ctrl+C 뒤 다시 시도) | `TestTrustPromptRecognised`, `TestServerUntrustedIsNotRetried` |
| R-11 | 10-27 | 관리자 권한 서비스가 떠 있으면 45초 기다림 → 바로 안내 | `TestLaunchRefusedWithElevatedService`, `TestServiceElevated` |

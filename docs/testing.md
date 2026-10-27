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

실제 에이전트와 실제 `claude`를 쓴다. 세션은 시험 폴더 `rc-test`에서만 만들고, 흐름이 끝날 때 지운다.

### 환경 (이 PC, 2026-10-28)

- `D:\Android`: JDK 17(Temurin), Android SDK, 에뮬레이터, Maestro 2.11.0(`D:\Android\maestro`). AVD `asksemble-play`(Android 35, **Google Play** 이미지, x86_64, 램 2GB, 4코어, 호스트 GPU)는 `D:\Android\avd`에 둔다(C:는 공간이 적다). 하드웨어 가속은 WHPX.
- 에뮬레이터 안에 **Tailscale 앱**을 깔고 PC와 같은 계정으로 로그인했다. 에뮬레이터가 tailnet 기기 하나로 붙어 MagicDNS 이름으로 에이전트에 닿는다(실제 폰과 같은 경로). E2E 전에 `e2e.ps1`이 PC 이름으로 ping해서 확인한다.
- Expo Go(58.0.5)는 앱 언어를 영어로 지정했다(`adb shell cmd locale set-app-locales host.exp.exponent --locales en-US`). 에뮬레이터 자체는 한국어다. 흐름은 영어 문구로 쓴다.
- 하드웨어 키보드를 켜 두어 화면 키보드가 없다. 흐름에 `hideKeyboard`를 쓰지 않는다(뒤로 가기로 동작해 화면을 닫음).
- 이 PC 화면이 작아(1280×680) 에뮬레이터 창이 화면 밖에 뜰 수 있다. `e2e.ps1`이 창을 화면 위쪽으로 옮긴다.
- 앱은 에뮬레이터의 Expo Go가 개발 서버 `exp://10.0.2.2:8081`에서 받는다. 개발 서버는 `scripts\dev-server.ps1`이 띄운다. 스크립트가 있는 작업 폴더의 앱을 보여 주므로, worktree의 변경은 그 worktree에서 띄워야 E2E에 들어간다.
- 에뮬레이터 앱은 에이전트와 짝 맺어 두었다(`_pair.yaml`, 링크는 환경 변수 `MAESTRO_LINK`, PC 허용 y는 사람이).
- **Claude 앱은 이 에뮬레이터에서 못 쓴다**: Play 프로텍트 인증이 '인증되지 않음'이라 Claude 앱 로그인(Google·이메일 모두)이 Play Integrity에서 막힌다. Claude 쪽 상태는 E2E-07처럼 PC Chrome의 claude.ai로 본다. Claude 앱으로 넘어가는 동작은 MT-02.

### 실행

```
powershell -NoProfile -File scripts\dev-server.ps1      # 개발 서버 창
powershell -NoProfile -File scripts\e2e.ps1             # 에뮬레이터를 띄우고 흐름 전부
powershell -NoProfile -File scripts\e2e.ps1 -Flow 02    # 하나만
```

`e2e.ps1`은 에뮬레이터가 없으면 창으로 띄우고(`-Headless`면 창 없이), Expo Go가 없으면 깔고, 흐름마다 최신 코드를 받도록 Expo Go를 새로 시작한다.

### 흐름 (`app/e2e`)

| ID | 파일 | 흐름 | 결과(10-27) |
| --- | --- | --- | --- |
| E2E-01 | `01-home-and-list.yaml` | 앱 열기 → PC 카드 '연결됨' → 세션 목록 | 통과 |
| E2E-02~04 | `02-new-stop-open-remove.yaml` | `rc-test`에 새 세션 → 목록에 '실행 중' → 멈추기('다시 열기' 사라짐) → 열기('멈춘 세션을 깨웠습니다') → 지우기 | 통과 |
| E2E-05 | `04-folder-keep-open.yaml` | 폴더 화면에서 `rc-test` 열어 두기 켬 → '열려 있음' → 끔 | 통과(R-12를 찾음) |
| E2E-06 | `03-settings-check.yaml` | 설정 → 연결 진단 → PC·짝·에이전트·Claude Code 모두 정상 | 통과 |
| E2E-07 | (PC Chrome, Claude in Chrome) | Claude 쪽 상태: rc-test에 새 세션 → claude.ai/code 목록에 'Remote Control · 연결됨' → 멈추기 → 목록에서 빠지고 링크는 '보관되었습니다' → 앱·에이전트로 다시 열기 → 보관이 풀리고 다시 '연결됨' → 지우기 | 통과(10-28) |

알아 둘 것:
- Expo Go는 앱을 열 때 개발자 메뉴를 늦게 띄우고, 오른쪽 위에 둥근 개발 도구 버튼을 둔다. 이 버튼이 첫 화면의 'Settings'를 덮어서, 설정은 링크(`exp://10.0.2.2:8081/--/settings`)로 연다. 실제로 빌드한 앱에는 이 버튼이 없다.
- 열기의 Claude 링크는 에뮬레이터 브라우저로 열린다(Claude 앱이 없음). 흐름은 뒤로 가기로 앱에 돌아와 결과를 본다. Claude 앱으로 넘어가는 것은 MT-02에서 본다.
- E2E-07은 제가 PC Chrome(Claude in Chrome 확장)으로 한다. 그 Chrome의 claude.ai 로그인을 쓰며, 읽기와 시험 세션(rc-test) 조작만 한다. 목록 항목은 `a[href*="session_…"]`의 글자('Remote Control · 연결됨/오프라인')로, 보관은 세션 페이지의 '이 세션은 보관되었습니다' 문구로 판단한다.
- 알게 된 것(10-28): `claude stop`으로 멈추면 Claude가 그 원격 세션을 **보관**하고 Code 목록에서 뺀다. 다시 깨워 연결이 붙으면 보관이 저절로 풀린다. 그래서 연결이 붙기 전에 링크를 열면 '보관됨'이 보인다(R-08).
- 실패한 흐름이 서버를 켠 채나 세션을 남긴 채 끝날 수 있다. 흐름은 시작 상태를 맞추고 시작한다(E2E-05). 남은 `e2e-…` 세션은 `asksemble-agent rm`으로 지운다.

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
| R-08 | 10-27 | 다시 깨운 세션의 링크를 다시 붙기 전에 줘서 '보관됨'(멈추면 Claude가 원격 세션을 보관하고, 다시 붙어야 풀린다. 10-28 E2E-07로 확인) | `TestOpenStoppedNeverHandsOutTheOldLink`, IT-04, E2E-07, MT-05 |
| R-09 | 10-27 | '다시 열기'가 멈춘 세션의 예전 링크를 그대로 엶 | `reopen-test.tsx` |
| R-10 | 10-26 | 신뢰 확인 창에 대신 y를 누르지 않음(Ctrl+C 뒤 다시 시도) | `TestTrustPromptRecognised`, `TestServerUntrustedIsNotRetried` |
| R-11 | 10-27 | 관리자 권한 서비스가 떠 있으면 45초 기다림 → 바로 안내 | `TestLaunchRefusedWithElevatedService`, `TestServiceElevated` |
| R-12 | 10-27 | 폴더 열어 두기를 켜도 화면이 'Off'에 머묾: 켠 직후 답에는 아직 예전 상태가 실려 오는데 화면은 '켜는 중'일 때만 다시 물었다 | E2E-05, `screens2-test.tsx` 폴더(가짜 에이전트가 실제처럼 늦게 켜짐) |

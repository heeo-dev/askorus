# 테스트

Asksemble Phase 1의 테스트 기준이다. 버그를 고치면 그 버그를 다시 일으키는 테스트를 같이 넣고, 아래 '회귀 표'에 적는다.

## 1. 단계

테스트는 피라미드 모양으로 둔다. 아래쪽(빠르고 안정적)을 많이, 위쪽(느리고 잘 깨짐)을 적게. 기능을 만들 때는 1단계로 빠르게 돌고, 2·3단계는 정해진 때만 돈다.

| 단계 | 무엇을 보나 | 어디 | 실행 | 언제 | 걸리는 시간 |
| --- | --- | --- | --- | --- | --- |
| **1. 매번** | UT: 함수·패키지 하나(에이전트·앱). IT: 에이전트 전체를 가짜 `claude`로 폰 순서대로. ST: 앱 화면 전체를 가짜 에이전트로 사람처럼 | `agent/**/*_test.go`, `scenario_it_test.go`, `app/src/**/*-test.ts(x)` | `scripts\test-all.ps1` | 고칠 때마다, 합치기·배포 전 | 2~5분, 에뮬레이터 없음 |
| **2. 실제 연동** | LT: 에이전트 + **실제 `claude`** + 실제 `~/.claude`, HTTP로 폰 순서대로. E2E-07: claude.ai 쪽 상태(목록·연결됨·보관됨) | `agent/internal/live/live_test.go`(태그 `live`), PC Chrome | `scripts\test-live.ps1`, E2E-07은 Claude in Chrome | 에이전트가 claude를 다루는 부분을 바꿨을 때 | 1~2분 |
| **3. 앱 전체** | E2E: 에뮬레이터의 Expo Go 앱 → Tailscale → 실제 에이전트 → 실제 `claude`. Android에만 있는 것(뒤로 가기, 링크 열기, 키보드, 화면 배치)과 앱·에이전트 연동 | `app/e2e/*.yaml`(Maestro) | `scripts\e2e.ps1` | 기본은 건너뜀. Phase 중간에 한두 번(10-29 사용자 결정) | 5분 + 부팅 2분 |
| **4. 실제 폰** | MT: 카메라 QR, Claude 앱으로 넘어가기, 폰 Tailscale on/off, PC 재부팅 | 5절 | 사람이 | 단계(B1, B2 …)를 마칠 때 | - |

- 가짜 `claude`: 테스트 실행 파일이 `ASKSEMBLE_FAKE_CLAUDE=1`일 때 `claude`처럼 동작한다(`fake_test.go`). 받은 인자를 기록하고 `agents --json`, `--bg`, `stop`, `rm`, `remote-control`을 흉내 내며, 환경 변수로 신뢰 안 됨·링크 없음·rm 한 번 실패 같은 상황을 만든다.
- 가짜 에이전트(앱): `app/src/test/fakeAgent.ts`. 실제 에이전트처럼 서버 상태가 한 박자 늦게 바뀐다(R-12).
- 시험 페이지(`/test/`)는 앱이 나오기 전 손으로 보던 도구다. 앱과 코드가 따로라 앱 버그(R-09, R-12)는 못 잡는다. 테스트 단계로 쓰지 않는다.

## 2. 회귀 규칙

1. **고칠 때마다·main에 합치기 전·`.local\bin`에 배포하기 전**: `scripts\test-all.ps1`이 `ALL PASSED`. gofmt, `go vet`(Windows·Linux), Go 전체(UT+IT), 앱 타입 검사, jest 전체(UT+ST), expo-doctor(`-Quick`이면 생략).
2. **에이전트가 claude를 다루는 부분(실행 인자, 상태 판정, 링크, 폴더 서버)을 바꾸면**: `scripts\test-live.ps1`이 `LIVE PASSED`. claude.ai 쪽 동작이 걸리면 E2E-07도 본다.
3. **앱을 바꾼 것을 main에 합치기 전**: `scripts\e2e.ps1`이 모두 Passed. 에뮬레이터는 창 없이 떴다가 끝나면 꺼진다(`-Window`로 보면서, `-Keep`으로 켜 둔 채).
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

### LT (실제 연동, `agent/internal/live/live_test.go`)

시험 폴더 `.local/rc-test`에서만 만들고 끝나면 지운다. 기기 토큰은 테스트가 임시 폴더에 스스로 만들어 짝 맺기가 필요 없다.

| ID | 시나리오 | 결과(10-28) |
| --- | --- | --- |
| LT-01 | `/v1/info`: Claude Code가 있고 PC가 알리는 문제가 없음 | 통과(2.1.311) |
| LT-02 | 실제 claude로 새 세션(22초) → 실행 중 → 멈춤 → 깨우기(17초, 다시 붙은 링크) → 실행 중 세션 열기는 바로 → 지우기 | 통과 |
| LT-03 | 실제 `claude remote-control` 폴더 서버 켜기 → running → 끄기(1.1초) | 통과 |
| LT-04 | 이 PC의 실제 기록 최근 40개 미리보기: 모양(질문 → 답, 최대 2개)과 시간. 개수와 시간만 기록하고 내용은 남기지 않음 | 통과(16개에 미리보기, 가장 느린 것 16ms) |

### ST (앱 화면, `app/src/test`)

| 파일 | 다루는 것 |
| --- | --- |
| `screens-test.tsx` | 첫 화면(PC 없음·연결됨·짝 끊김), 짝 맺기(붙여넣기·딥 링크·거절·잘못된 링크), 세션 목록(배지·필터·검색이 글자마다 묻지 않음), 열기→Claude 앱 링크, 멈추기는 실행 중일 때만, 지우기 확인, 폴더 없음, 짝 끊김 |
| `screens2-test.tsx` | 새 세션(폴더·이름·모델, 설정 기본값, 폴더 없이 시작 불가, 폴더가 없을 때 안내), 폴더 열어 두기, 설정(연결 진단, PC가 알리는 문제, 끊기, PC에 닿지 않을 때 끊기, 설정으로 가기) |
| `history-test.tsx` | 기록 폴더: 새 세션 화면에 따로 보이고 그 폴더로 시작, 열어 두기를 켜면 허용 목록으로 옮김 |
| `errors-test.tsx` | 원인별 오류(에이전트 꺼짐·닿지 않음·다시 시도), 첫 화면 한 줄 원인, PC 알림, PC 두 대(F7), '한 번에 묻기' 숨김 |
| `reopen-test.tsx` | '다시 열기'와 멈춘 뒤 열기가 매번 PC에 묻기(R-09) |
| `preview-test.tsx` | 세션 시트의 '최근 대화'(F8): 질문·답·도구 종류별 횟수(모르는 종류는 코드 그대로, 예전 에이전트는 합계), 아직 답이 없음, PC가 못 읽으면 조용히 예전 '마지막 질문' 줄, `preview`를 모르는 예전 에이전트에는 묻지 않음 |

## 4. E2E (에뮬레이터)

실제 에이전트와 실제 `claude`를 쓴다. 세션은 시험 폴더 `rc-test`에서만 만들고, 흐름이 끝날 때 지운다.

### 환경 (이 PC, 2026-10-28)

- `D:\Android`: JDK 17(Temurin), Android SDK, 에뮬레이터, Maestro 2.11.0(`D:\Android\maestro`). AVD `asksemble-play`(Android 35, **Google Play** 이미지, x86_64, 램 2GB, 4코어, 호스트 GPU)는 `D:\Android\avd`에 둔다(C:는 공간이 적다). 하드웨어 가속은 WHPX.
- 에뮬레이터 안에 **Tailscale 앱**을 깔고 PC와 같은 계정으로 로그인했다. 에뮬레이터가 tailnet 기기 하나로 붙어 MagicDNS 이름으로 에이전트에 닿는다(실제 폰과 같은 경로). E2E 전에 `e2e.ps1`이 PC 이름으로 ping해서 확인한다.
- Expo Go(58.0.5)는 앱 언어를 영어로 지정했다(`adb shell cmd locale set-app-locales host.exp.exponent --locales en-US`). 에뮬레이터 자체는 한국어다. 흐름은 영어 문구로 쓴다.
- 화면 키보드는 실제 폰처럼 켜 둔다(`adb shell settings put secure show_ime_with_hard_keyboard 1`, 10-28부터). 입력 칸 뒤의 버튼은 흐름이 키보드의 완료(`pressKey: Enter`)로 키보드를 닫고 누른다. `hideKeyboard`는 쓰지 않는다(뒤로 가기로 동작해 화면을 닫음).
- 이 PC 화면이 작아(1280×680) 에뮬레이터 창이 화면 밖에 뜰 수 있다. `e2e.ps1`이 창을 화면 위쪽으로 옮긴다.
- 앱은 에뮬레이터의 Expo Go가 개발 서버 `exp://10.0.2.2:8081`에서 받는다. 개발 서버는 `scripts\dev-server.ps1`이 띄운다. 스크립트가 있는 작업 폴더의 앱을 보여 주므로, worktree의 변경은 그 worktree에서 띄워야 E2E에 들어간다.
- 에뮬레이터 앱은 에이전트와 짝 맺어 두었다(`_pair.yaml`, 링크는 환경 변수 `MAESTRO_LINK`, PC 허용 y는 사람이). 10-28 `eas init`으로 app.json에 `owner`·`projectId`가 생긴 뒤 Expo Go의 앱 저장소가 바뀌어 짝 정보가 안 보였고, 다시 짝 맺었다(PC에 `asksemble-agent pair` 창을 띄우고 출력의 링크를 `MAESTRO_LINK`로).
- 에뮬레이터를 새로 띄우면 안의 Tailscale이 바로 붙지 않을 때가 있다. Tailscale 앱을 열면(`adb shell monkey -p com.tailscale.ipn -c android.intent.category.LAUNCHER 1`) 30~40초 안에 붙는다. 새 worktree에서 처음 묶는 번들은 약 30초 걸려 첫 흐름이 실패할 수 있다(한 번 열어 데운 뒤 돌린다).
- **Claude 앱은 이 에뮬레이터에서 못 쓴다**: Play 프로텍트 인증이 '인증되지 않음'이라 Claude 앱 로그인(Google·이메일 모두)이 Play Integrity에서 막힌다. Claude 쪽 상태는 E2E-07처럼 PC Chrome의 claude.ai로 본다. Claude 앱으로 넘어가는 동작은 MT-02.

### 실행

```
powershell -NoProfile -File scripts\dev-server.ps1      # 개발 서버 창
powershell -NoProfile -File scripts\e2e.ps1             # 에뮬레이터를 띄우고 흐름 전부
powershell -NoProfile -File scripts\e2e.ps1 -Flow 02    # 하나만
```

`e2e.ps1`은 에뮬레이터가 없으면 **창 없이** 띄우고(`-Window`면 창으로), Expo Go가 없으면 깔고, 에뮬레이터 안의 Tailscale이 PC에 닿을 때까지 기다리고, 최신 코드를 받도록 Expo Go를 새로 시작한다. 직접 띄운 에뮬레이터는 끝나면 끈다(`-Keep`이면 둔다). 창 없이 띄워도 램은 비슷하게 쓰고(약 2.5GB), 화면을 그리는 CPU·GPU만 준다. 큰 이득은 끝나면 끄는 것이다.

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
| MT-05 | PC 재부팅 뒤: 에이전트 자동 시작, 꺼진 세션 열기가 '보관됨' 없이 열림(R-08). 10-29 통과(재부팅 뒤 앱에서 이 개발 세션을 열어 이어 씀) |
| MT-06 | 열어 두기 켠 폴더로 Claude 앱 PC 목록에서 새 세션 |
| MT-07 | 설치본(EAS 빌드, B2부터): 폰 다크 모드를 따름(R-14), 앱을 껐다 켜도 짝이 남음, 권한은 카메라만 물음 |
| MT-08 | 설치본: `pair`가 띄운 QR(`asksemble://pair?…`)을 폰 기본 카메라 앱으로 찍으면 이 앱의 짝 맺기 화면이 열림 |

E2E는 Expo Go로 돌아서 설치본에서만 드러나는 차이(다크 모드, 권한, 딥 링크 scheme)를 못 본다. 이런 설정은 `app/src/config-test.ts`(UT)로 지키고, 설치본은 MT-07·08로 본다.

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

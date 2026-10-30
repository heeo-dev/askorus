# Phase 2 구현 계획 — 한 번에 묻기

2026-10-29 작성. 기준은 Claude Docs '제품 방향과 Phase 설계'의 'Phase 2 — 한 번에 묻기' 절이다. Phase 1의 E(2차)에서 F8은 끝났고, F9(우리 쪽 알림)와 `tsnet`은 미뤘다(사용자 결정, `phase1-plan.md` 18절, Phase 1 설계 rev 65).

## 1. Phase 3를 먼저 하지 않는 이유

Phase 2가 다른 AI에게 쓰는 것은 '질문 한 번(ask)'뿐이다. 명령 하나를 실행하고 답을 받는다. Phase 3의 세션 목록·재개·앱 안에서 보기는 AI마다 기록 형식과 상태를 다뤄야 해서 훨씬 크고, Phase 2의 ask 위에 쌓인다. 에이전트는 Phase 1부터 AI마다 어댑터 하나(원칙 1~3)라 다른 AI를 붙일 자리가 있다.

## 2. Phase 1 원격 세션과 Phase 2 묻기의 차이 (2026-10-29 정리)

둘 다 같은 Claude 대화 기록 형식 위에서 돌지만 프로세스를 띄우는 방식과 대화하는 곳이 다르다. 헷갈리지 않게 적어 둔다(사용자가 이해한 것: "Phase 2 세션은 늘 1회용이고 필요할 때 다시 resume, Phase 1은 사용자가 멈추기 전까지 `--bg`로 열려 있는 원격 세션" — 맞다).

| | Phase 1 원격 세션 | Phase 2 한 번에 묻기 |
| --- | --- | --- |
| 실행 | `claude --bg --remote-control` | `claude -p`(후속 질문은 `-p --resume <id>`), Codex·OpenCode도 같은 식 |
| 프로세스 | 멈추기 전까지 PC에 떠 있음 | 한 번 답하고 바로 끝남. 질문마다 새로 떴다가 끝남 |
| 대화하는 곳 | Claude 앱(원격 링크로 넘어감) | 우리 앱. 에이전트가 답 글자를 받아 폰에 전한다 |
| 하는 일 | 코딩 에이전트 전부(파일 고치기, 명령 실행, 권한 묻기) | 답만 한다. 파일을 고치거나 명령을 실행하지 못하게 막는다(P0-2) |
| 대화를 잇는 법 | 프로세스가 살아 있어 그대로 | 기록 파일에 남은 대화를 resume으로 다시 불러온다 |
| AI | Claude만 | Claude·Codex·OpenCode |

- 후속 질문마다 프로세스가 새로 뜨고 앞 대화를 다시 읽어 첫 글자까지 몇 초씩 더 걸린다(P0-3에서 잰다).
- 기록 형식이 같아서, Phase 2에서 Claude에게 물은 대화를 Phase 1 방식(`--bg --rc`로 재개)으로 열어 "이 답대로 코드를 고쳐 줘"처럼 코딩 세션으로 넘길 수 있다. 당장은 하지 않고 Phase 3 즈음에 본다.

### 후속 질문 (사용자 결정: A)

| 방법 | 어떻게 | 정한 것 |
| --- | --- | --- |
| **A. AI마다 대화를 이어 감** | 첫 질문 때 각 CLI가 만든 세션 id를 폰 기록에 두고, 후속 질문은 그 세션을 resume | **채택(2026-10-29)**. 사용량이 적고 AI가 자기 앞 답을 기억한다 |
| B. 매번 앞 대화를 붙여 새로 물음 | "앞 질문·답 + 새 질문"을 한 번에 | 쓰지 않음(길어질수록 느리고 사용량이 는다). resume이 안 되는 AI의 예비책으로만 |

- 후속 질문을 누구에게: 셋 모두, 한 AI만, '더 따져 보기'(각 AI가 다른 AI의 답을 읽고 짚음) 중 화면에서 고르게 한다(추천).
- `claude -p`로 물은 것도 Claude 세션으로 기록되어 Phase 1 세션 목록에 섞인다. 묻기 세션은 표시를 붙여 목록에서 거른다(P0-8에서 방법 확인).

### 모드: ask · work · code (2026-10-29 사용자 결정)

| 모드 | 남기는 것 | 일하는 곳 | 도구·권한 | 사람이 확인 | 입구 | Phase |
| --- | --- | --- | --- | --- | --- | --- |
| `ask` 묻기 | 답(글) | 빈 묻기 폴더 | 웹 검색·읽기만. 파일 쓰기·명령 없음 | 답 비교 | 입구 1(모두) | 2 |
| `work` 일 맡기기 | 파일(문서·PPT·md·표) | 작업마다 새 결과 폴더, git 없음 | 결과 폴더 안 쓰기 | 나눈 계획, 결과 파일 | 입구 1(모두) | 4 |
| `code` 코딩하기 | 코드 변경 | 내 저장소의 AI별 git worktree | worktree 안 쓰기, 권한 요청은 사람에게 | 계획, 권한 요청, diff, 합치기 | 입구 2(내 PC, 개발자) | 4 |

- research는 따로 두지 않는다. 웹 검색은 묻기에 원래 있어야 한다(사용자). 그래서 P0-7은 "묻기에 웹 검색·읽기를 기본으로 켬"으로 정해졌고, P0-2의 Claude `--tools ""`는 `--tools "WebSearch,WebFetch"` 같은 꼴로 바뀐다. AI별 웹 검색 옵션은 4절 P0-7에서 확인했다(2026-10-30).
- 실행 틀(CLI 띄우기, 출력 읽기, 세션 id·이어 가기, PWD·폴더, 동시 실행·사용량, 작업 id, 기록)은 세 모드가 같이 쓰고, 도구·권한·폴더 정책만 모드별로 둔다. 모드와 상관없이 권한을 푸는 옵션은 넘기지 않고, 권한 요청을 자동으로 승인하지 않는다.
- API는 `POST /v1/runs`에 `mode`를 받는다. Phase 2에서는 `ask`만 만들고 `work`·`code`는 자리만 둔다(`/v1`은 더하기만). 묻기 배지도 모드 배지(`ask`·`work`·`code`)로 넓힌다.
- 두 입구와 하위 메뉴, 한국어·영어 이름은 추천대로 확정(2026-10-29): 입구 1 '한 번에 / All at once'(묻기 Ask · 일 맡기기 Work), 입구 2 '내 PC / My PCs'(원격 세션 · 코딩하기 Code). 흐름도 원본은 `docs/menu/menu-pc.html`·`docs/menu/menu-mobile.html`이고 설계와 함께 유지한다(CLAUDE.md). 원본 설계 '제품 방향과 Phase 설계' rev 72에 반영.

## 3. 이 PC에 있는 것 (2026-10-29)

| AI | 명령 | 버전 | ask에 쓸 명령(설계) |
| --- | --- | --- | --- |
| Claude Code | `claude` | 2.1.312 | `claude -p` |
| Codex CLI | `codex` | 0.164.0 | `codex exec` |
| OpenCode | `opencode` | 2.2.0 | `opencode run` |
| Antigravity CLI | `agy` | 없음 | `agy -p` |
| Gemini CLI | `gemini` | 없음 | — |

## 4. 시작 전에 확인할 것 (설계 'Phase 2 전에 확인할 것')

| # | 확인 | 누가 | 방법 |
| --- | --- | --- | --- |
| P0-1 | ask 명령마다 답만 받는 방법: 출력 형식(JSON 옵션), 끝나는 신호, 오류·로그인 안 됨일 때 | Claude | 빈 시험 폴더에서 짧은 질문 하나. 권한을 푸는 옵션은 쓰지 않는다 |
| P0-2 | ask가 파일을 고치거나 명령을 실행하지 못하게 막는 방법(읽기 전용·도구 끄기) | Claude | 각 CLI 도움말과 실제 시험 |
| P0-3 | 셋을 동시에 돌릴 때 걸리는 시간과 구독 사용량 | Claude | 같은 질문 3개를 동시에, 시간 재기 |
| P0-4 | 공식 앱으로 넘기기: ChatGPT·Claude·Gemini 앱이 공유로 받은 글을 입력창에 넣는지, 답을 공유로 내보낼 수 있는지 | 사용자(폰), Claude(테스트 폰) 교차 확인 | 폰에서 공유 시험. **끝(2026-10-30, Android), 아래 P0-4 절** |
| P0-5 | `agy`를 설치해 넣을지, 일단 셋으로 시작할지 | 사용자 | **셋으로 시작(2026-10-29)** |
| P0-6 | 판정 검증용 예시 질문 30개(한국어 포함)와 사람이 단 정답 | 사용자 + Claude | Claude가 초안, 사용자가 정답 확인 |
| P0-7 | 웹 검색이 필요한 질문에서 각 CLI의 검색을 켤지 | 함께 | 묻기에 기본으로 켬(2절 모드 결정). **옵션 확인 끝(2026-10-30), 아래 P0-7 절** |
| P0-8 | 후속 질문 이어 가기(resume)와 묻기 세션을 Phase 1 목록에서 거르는 법 | Claude | CLI마다 실제 시험 |
| P0-9 | 사용자 전역 지시(AGENTS.md 등)가 묻기 답에 섞이지 않게 하는 법 | Claude | 아래 '속도 비교'의 남은 것 |

### P0-1·P0-3 첫 시험 (2026-10-29)

빈 시험 폴더 `.local\ask-test`에서 같은 짧은 한국어 질문("대한민국의 수도는 어디인가요? 한 문장으로만 답하세요.")을 셋에 동시에 보냈다. 셸 없이 인자 배열로 실행했고 권한을 푸는 옵션은 쓰지 않았다.

| AI | 명령 | 결과 | 시간 | 알게 된 것 |
| --- | --- | --- | --- | --- |
| Claude | `claude -p <질문> --output-format json` | 맞게 답함 | 14.4초 | JSON 하나: `result`(답), `session_id`(resume에 씀), `modelUsage`(claude-opus-5-5), `total_cost_usd` 0.084(구독이라 실제 청구는 아님, 환산값) |
| Codex | `codex exec -s read-only --json <질문>` | 맞게 답함 | 14.9초 | JSONL 이벤트: `thread.started`(thread_id = resume에 씀), `item.completed`의 `agent_message`(답), `turn.completed`의 usage(입력 17,634 토큰 중 17,408 캐시). stdin을 닫아 두면 "Reading additional input from stdin..."만 찍고 진행 |
| OpenCode | `opencode run --format json <질문>` | **거절** | 19.6초 | 모델을 지정하지 않으면 OpenCode가 고른 Go 모델로 가고, 그 모델은 "요청 데이터로 학습하는 유료 엔드포인트" 허용이 필요하다며 400. 설정(`opencode debug config`)에 `model`이 없다 |
| OpenCode | `opencode run --format json -m opencode-go/deepseek-v4.1-flash <질문>` | 답함 | — | **기본값은 DeepSeek V4.1 Flash가 아니다.** 상태 파일 `model.json`의 최근·즐겨찾기에는 `opencode-go/deepseek-v4.1-flash`(생각 max)가 있지만 `run`은 그걸 기본으로 쓰지 않는다. 그래서 에이전트는 모델을 늘 직접 넘긴다(기본 `opencode-go/deepseek-v4.1-flash`, 설정으로 바꿈) |

- 셋을 동시에 돌려도 각자 15~20초. 가장 느린 AI가 전체 시간을 정한다.
- 한국어 답은 UTF-8로 잘 나온다(코드값으로 확인). 이 PC의 bash 화면에서만 깨져 보였다.
- 로그인 정보 파일(OpenCode `auth.json` 등)은 열지 않았다. 모델 확인에는 `opencode models`, `opencode debug config`, 상태의 `model.json`(최근 모델 목록)만 봤다.

### 묻기용 모델 (2026-10-29 사용자 결정)

평소 코딩에 쓰는 각 CLI 설정(Claude `opus`, Codex `gpt-6.1-terra` xhigh)은 그대로 두고, 묻기는 에이전트가 모델과 생각 수준을 직접 넘긴다. 세 단계 중 **기본**이 기본값이다.

| 단계 | Claude | Codex | OpenCode |
| --- | --- | --- | --- |
| 빠르게 | `--model haiku --effort medium` | `-m gpt-6-luna -c model_reasoning_effort="medium"` | `-m opencode-go/deepseek-v4.1-flash#high` |
| **기본** | `--model sonnet --effort medium` (Sonnet 5.5) | `-m gpt-6.1-terra -c model_reasoning_effort="medium"` | `-m opencode-go/deepseek-v4.1-flash#max` |
| 깊게 | `--model opus --effort medium` | `-m gpt-6.1-sol -c model_reasoning_effort="medium"` | `-m opencode-go/kimi-k3.1-pro#high`(medium이 없어 high로, 사용자 결정) |

- 2026-10-29 사용자 결정: 단계는 모델로 나누고 생각 수준은 medium으로 맞춘다. OpenCode만 DeepSeek high / DeepSeek max / Kimi K3.1 Pro로.
- OpenCode 변형(`#이름`)은 모델마다 다르다. Kimi K3.1 Pro는 `low`·`high`·`max`와 변형 없음만 되고 `medium`·`xhigh`는 "Variant unavailable"로 거절된다(짧은 질문으로 하나씩 시험). 변형 없이 부르면 OpenCode가 정한 기본 수준으로 돈다.

- Codex의 Luna는 6.1이 없어 `gpt-6-luna`(최신)를 쓴다. 모델 이름은 `~/.codex/models_cache.json`과 `opencode models`로 확인했다.
- 단계와 모델은 에이전트 설정으로 바꿀 수 있게 하고, 앱에서는 질문마다 고른다(5절 P2-2).

### 속도 비교 (2026-10-29, "HTTP와 HTTPS의 차이를 두 문장으로 설명하세요.", 단계마다 셋을 동시에)

| 단계 | 폴더 | Claude | Codex | OpenCode | 전체(가장 느린 것) |
| --- | --- | --- | --- | --- | --- |
| 빠르게 | `D:\ClaudeHome` 아래 | 15.6초 | 16.9초 | 14.7초 | 16.9초 |
| 기본 | `D:\ClaudeHome` 아래 | 14.6초 | 16.2초 | 7.9초 | 16.2초 |
| 깊게 | `D:\ClaudeHome` 아래 | 15.2초 | 16.4초 | 17.3초 | 17.3초 |
| 빠르게 | 중립 폴더 | 9.9초 | 10.8초 | 5.0초 | **10.8초** |
| 기본 | 중립 폴더 | 10.1초 | 13.6초 | 5.9초 | **13.6초** |
| 깊게 | 중립 폴더 | 9.4초 | 12.6초 | 16.0초 | **16.0초** |

바꾼 설정으로 다시(2026-10-29, 중립 폴더): 빠르게 10.5초, 기본 **9.7초**, 깊게 11.4초(Kimi는 `#medium`이 없어 이 회차에서 빠짐). Kimi `#high`로 깊게만 다시: Claude 9.8초, Codex 12.9초, Kimi 13.4초, 전체 **13.4초**(세 답 모두 정상, 덧붙인 문장 없음). AI별로는 Claude 8.5~10.5초, Codex 9.6~11.4초, OpenCode DeepSeek 6.9~7.1초. 생각 수준을 medium으로 낮추자 Codex가 13.6초 → 9.6초로 줄었다.

알게 된 것:

- **묻기는 코딩 폴더 밖에서 돌린다.** `D:\ClaudeHome` 아래에서 물으면 각 CLI가 그 폴더의 지시 파일(CLAUDE.md 등)을 읽어, 답 끝에 '다음에 쓸 문장'을 붙이고 Claude는 "Asksemble 다음 작업으로 F9 진행해 줘"까지 제안했다. 개발 맥락이 일반 질문에 섞이는 것이고 느리기도 하다. 중립 폴더(`%LOCALAPPDATA%\Asksemble\ask`)로 옮기자 Claude는 깨끗해지고 15초 → 9~10초로 빨라졌다.
- 짧은 질문에서는 단계 차이보다 CLI가 뜨는 시간(약 5~10초)이 크다. 빠르게와 기본의 차이는 몇 초다. 긴 질문에서는 다를 수 있어 판정 검증(P0-6) 때 다시 잰다.
- Codex는 git 저장소 밖에서 `--skip-git-repo-check`가 있어야 실행된다(검사만 끄는 옵션, 권한을 풀지 않음). 읽기 전용(`-s read-only`)은 그대로 둔다.
- **아직 남은 것(P0-9):** Codex와 OpenCode는 사용자 전역 지시(`~/.codex/AGENTS.md`, `~/.config/opencode/AGENTS.md`)를 중립 폴더에서도 읽어, 9개 답 중 3개에 '다음에 쓸 문장'이 붙었다. Claude의 `--bare`는 지시 파일을 건너뛰지만 구독 로그인도 쓰지 않아(API 키만) 쓸 수 없다. 후보: 질문 앞에 "질문에만 답하고 덧붙이지 말 것" 같은 짧은 지시를 붙이기, CLI별로 전역 지시를 끄는 방법 찾기.

### P0-9 전역 지시가 섞이는 것 (2026-10-29)

| AI | 읽는 전역 지시 | 막아 본 것 | 결과 |
| --- | --- | --- | --- |
| Claude | 없음(`~/.claude/CLAUDE.md` 없음) | 중립 폴더에서 묻기 | 해결. 직접 물어도 "NO" |
| Codex | `~/.codex/AGENTS.md` | `-c project_doc_max_bytes=0` | 안 됨(프로젝트 폴더 것만 막음, 전역은 그대로 "YES") |
| OpenCode | `~/.config/opencode/AGENTS.md` | 이 실행만 `XDG_CONFIG_HOME`을 빈 폴더로 | 안 됨(Windows에서는 홈 폴더 기준으로 찾음, "YES") |

- `CODEX_HOME`이나 홈 폴더를 바꾸면 로그인 정보까지 옮겨야 해서 쓰지 않는다(N10).
- **해결: 질문 앞에 짧은 머리말을 붙인다**(10-30에 영어로 바꿈, 아래). 처음 것: "[Asksemble] 이 질문은 다른 앱이 여러 AI에게 같이 보낸 일반 질문입니다. 코딩 작업이 아닙니다. 질문에만 답하고, 다음에 할 일 제안·추천 문장·'다음에 쓸 문장' 같은 덧붙임 절은 쓰지 마세요." 셋을 세 번씩 돌려 덧붙임이 붙은 답을 셌다.

| | Claude | Codex | OpenCode |
| --- | --- | --- | --- |
| 머리말 없이 | 0/3 | 0/3(앞 회차에서는 1/3) | **3/3** |
| 머리말 붙임 | 0/3 | 0/3 | **0/3** |

- 머리말은 셋 모두에 같은 글로 붙인다(AI마다 다르게 할 이유가 없고 단순함). 기록에는 머리말이 같이 남으므로 앱은 자기 기록(폰)에서 질문만 보여 준다. 후속 질문에도 붙인다.
- 사용자 쪽 선택지: 전역 AGENTS.md의 그 규칙을 "Claude 원격으로 개발할 때만"처럼 좁히면 머리말 없이도 된다. 머리말은 그와 상관없이 다른 사용자를 위해 둔다.

**머리말을 영어로 바꿈 (2026-10-30, P2-2 폰 시험에서 짧은 영어 후속에 Claude가 한국어로 답한 뒤).** 실제 에이전트와 같은 명령('빠르게' 단계, stdin, 묻기 폴더 안 `ask\exp`)으로 머리말만 바꿔 셋에 물었다. 시나리오 E는 영어 질문 "Explain HTTP vs HTTPS in two sentences." → 후속 "Now say it in one short sentence.", K는 한국어 질문 → 후속 "방금 답을 한 문장으로 줄여 주세요." 칸은 질문 언어와 다른 언어로 온 답 / 덧붙임이 붙은 답(대화 하나에 답 둘).

| 머리말 | Claude | Codex | OpenCode |
| --- | --- | --- | --- |
| 한국어(그때까지 것), E×2 K×1 | **언어 1/6**(E 후속이 한국어) · 덧붙임 0/6 | 0/6 · 0/6 | 0/6 · 0/6 |
| 없음, E×2 K×1 | 0/6 · 0/6 | 언어 0/6이나 E 답에 한국어 '다음에 쓸 문장' · **덧붙임 5/6** | 0/6 · **덧붙임 2/6** |
| 영어 "in the language the question is written in", E×2 K×2 | 0/8 · 0/8 | **언어 2/8**(영어 질문 대화 하나가 통째로 한국어) · 0/8 | 0/8 · 0/8 |
| **영어, 다른 지시가 달리 말해도 질문 언어로(채택)**, E×3 K×2 | **0/10 · 0/10** | **0/10 · 0/10** | **0/10 · 0/10** |

- Claude가 한국어로 답한 원인은 한국어 머리말이었다(머리말 없이·영어 머리말로는 영어로 답함). Claude에는 전역 지시 파일이 없다.
- Codex가 한국어로 기우는 원인은 사용자 전역 `~/.codex/AGENTS.md`(한국어로 쓰라는 규칙)다. 그래서 "질문 언어로 답하라"만으로는 한 번 졌고, "다른 지시가 다른 언어를 말해도"를 넣자 10/10 지켰다. 묻기에서만 질문 언어를 따르는 것이고, 사용자의 코딩 세션 지시는 그대로다.
- P0-9(덧붙임)는 영어 머리말로도 0이다. 채택한 머리말은 `agent/internal/core/ask.go`의 `AskPreamble`. 시험 대화는 묻기 폴더 안 `ask\exp`에 남았다(`ask` 배지가 붙고 새 세션 폴더에는 안 나옴).

### P0-2 묻기가 파일을 고치지 못하게 (2026-10-29)

중립 폴더에서 각 AI에게 "도구로 `ask-write-test.txt`를 만들라"고 시키고 파일이 생기는지 봤다(생긴 파일은 바로 지움).

| AI | 실행 | 파일 생김 | 결과 |
| --- | --- | --- | --- |
| Claude | `-p` 그대로 | 아니오 | `-p`는 묻지 않고 거절함 |
| Claude | `-p --tools ""` | 아니오 | **채택.** 도구를 모두 끈다. 사용자 설정의 허용 규칙과 상관없이 막힌다(P0-7에서 웹 도구 둘만 켜는 것으로 바꿈) |
| Codex | `exec -s read-only` | **예** | 사용자 설정의 `approvals_reviewer = "auto_review"`가 권한 올림 요청을 자동으로 승인한 것으로 보인다 |
| Codex | `exec -s read-only --ignore-user-config` | 아니오 | **채택.** `config.toml`만 빼고 로그인은 그대로 쓴다. 모델·생각 수준은 어차피 직접 넘긴다 |
| OpenCode | `run`(기본 build 에이전트) | **예** | 쓸 수 있다 |
| OpenCode | `run --agent plan` | 아니오 | 막히지만 계획을 세우는 성격이라 일반 질문에 1,869자로 길게 답한 적이 있다 |
| OpenCode | `run --standalone --agent asksemble-ask` + `OPENCODE_CONFIG_CONTENT` | 아니오(이 시험) | 이 실행에만 묻기 전용 에이전트(편집·셸 금지, "질문에만 답하고 덧붙이지 말 것")를 넘긴다. **2026-10-30에 다시 보니 하위 에이전트(`subagent`)를 불러 파일을 썼다 → 권한을 '모두 금지 + 웹만 허용'으로 바꿈(P0-7 절).** 사용자 설정 파일은 건드리지 않는다. 머리말 없이도 덧붙임 0/3 |

- **OpenCode는 실행 폴더 대신 환경 변수 `PWD`를 따른다.** Git Bash에서 띄운 시험에서는 `PWD`가 worktree라 파일이 worktree에 생겼다(지움). 에이전트는 OpenCode를 띄울 때 `PWD`를 묻기 폴더로 꼭 넣는다. 그래서 앞의 OpenCode 시험 일부는 사실 `D:\ClaudeHome` 아래에서 돌았다. 다시 재도 전역 AGENTS.md 때문에 덧붙임 3/3이라 P0-9 결론은 같다.
- `OPENCODE_CONFIG_CONTENT`는 백그라운드 서비스에 붙는 `run`에는 전해지지 않아("Agent not found") `--standalone`(이 실행만의 서버)이 필요하다. 시간: 서비스 3.5·4.5초, standalone 6.2·7.4초(+2~3초, 여전히 셋 중 가장 빠름).
- 권한을 푸는 옵션(`--auto`, `--dangerously-*`, `bypassPermissions`)은 어디에도 쓰지 않았다.

### P0-8 묻기 세션을 Phase 1 목록에서 거르기 (2026-10-29)

- 오늘 시험한 묻기 세션 18개가 Phase 1 세션 목록에 '기록만'으로, 묻기 폴더가 '최근 대화한 폴더'로 보였다.
- 기록의 `entrypoint`는 `-p`가 `sdk-cli`, 터미널·`--bg`가 `cli`였다. 하지만 폴더 서버(F10)가 띄우는 세션도 `--print --sdk-url`로 돌아 같은 값일 수 있어 이걸로 거르지 않는다.
- 처음 안은 묻기 폴더의 세션을 목록에서 빼는 것이었으나, **사용자 결정(2026-10-29)으로 숨기지 않고 태그를 단다.** 묻기 대화도 평범한 Claude 기록이라 Phase 1에서 '열기'로 이어 코딩 세션으로 넘길 수 있기 때문이다(2절의 연결).
- **에이전트:** 기록의 작업 폴더가 묻기 전용 폴더(`%LOCALAPPDATA%\Asksemble\ask`, 설정으로 바꿈) 안이면 세션에 배지 `ask`를 단다('기록만' 같은 상태는 그대로). `badges`에 값을 더하는 것이라 `/v1` 규칙에 맞는다. 판단은 폴더로 한다(`entrypoint`는 폴더 서버 세션과 겹칠 수 있어 쓰지 않음). Codex·OpenCode 세션은 Phase 1 목록에 없어 상관없다.
- **앱 Phase 1 세션 목록:** 상태 필터 칩 옆에 **'묻기 대화'** 칩을 둔다. 기본으로는 `ask` 세션을 빼고 보이고, 칩을 누르면 `ask` 세션만 보인다. 세션 카드와 시트에는 '묻기' 배지.
- **새 세션 화면의 '최근 대화한 폴더'와 폴더 화면:** 묻기 폴더는 뺀다. 코드가 없는 빈 폴더라 새 코딩 세션을 열 일이 없다.
- **'모두 묻기 대화'의 주된 자리는 한 번에 묻기 탭의 기록(폰)이다.** 질문 하나에 세 AI의 답이 묶여 있는 곳은 그곳뿐이고, Phase 1 목록에는 Claude 쪽 반쪽만 있다. 묻기 탭의 Claude 답 카드에 'Claude 앱에서 이어서 하기'를 두어 두 곳을 잇는다(그 세션을 Phase 1 열기 규칙으로 깨워 Claude 앱으로 넘김).
- 오늘 시험으로 생긴 묻기 세션 18개도 이 배지가 생기면 '묻기 대화'로 옮겨 보인다.

### P0-4 공식 앱으로 넘기기 (2026-10-29 사용자 폰, 2026-10-30 테스트 폰 교차 확인)

시험 1은 공유(SEND)로 질문 글 보내기, 시험 2는 주소에 질문 넣기(`?q=`), 시험 3은 답 아래 공유 버튼, 시험 4는 답 아래 복사 버튼 → 메모 앱(ColorNote)에 붙여 넣기다. 사용자가 자기 폰에서 1~3을 했고, Claude가 테스트 폰(Galaxy S10, Android 12)에서 1~4를 다시 했다. 질문은 "대한민국의 수도는 어디인가요? 한 문장으로 답하세요."이고, 시험 4의 서식은 굵게·글머리표·표를 시킨 질문으로 따로 봤다. 패키지는 ChatGPT `com.openai.chatgpt`, Claude `com.anthropic.claude`, Gemini `com.google.android.apps.bard`(화면은 Google 앱 안에서 뜬다).

| 시험 | ChatGPT | Claude | Gemini |
| --- | --- | --- | --- |
| 1 공유로 질문 보내기 | 새 대화 입력창에 들어가 기다린다 | 먼저 Claude의 '보낼 곳(Send to)'이 뜬다: 새 세션, '늘 새 세션으로' 스위치, 최근 대화. 고르면 입력창에 들어가 기다린다. 최근 대화를 고르면 그 대화에 이어서 넣는다 | 새 대화 입력창에 들어가 기다린다(늘 새 대화) |
| 2 주소로 질문 넣기 | `https://chatgpt.com/?q=…` 입력창에 들어가 기다린다. Chrome에서도, 앱을 정하지 않고 열면 앱에서도(앱 링크) | `https://claude.ai/new?q=…` 앱을 정하지 않고 열면 앱의 새 대화 입력창에 들어가 기다린다. 이때만 "보내기 전에 조심하라(악성 내용이 Claude를 속일 수 있다)"는 경고가 붙는다 | **안 된다.** `?q=`, `?prompt=` 모두 무시(웹도 앱도 빈 입력창) |
| 3 답 아래 공유 | ChatGPT 자체 공유 시트 → 링크만(`chatgpt.com/s/t_…`). 링크를 열면 질문 없이 그 답 하나 | '스냅샷 공유' 시트. 기본이 '나만'이고 '링크 공유'가 꺼져 있다. '링크가 있는 누구나'(공개)로 바꿔야 링크가 생기고, "이 지점까지의 메시지만 공유된다" | 시스템 공유 시트가 바로 뜬다(Copy URL) → 링크만(`share.gemini.google/…`). 링크를 열면 질문과 답 한 쌍, '게시됨' 표시 |
| 4 답 복사 | 답만(질문 없음). 굵게는 빠지고, 목록은 `* `(줄 사이 빈 줄), 표는 마크다운 표 | 답만. 마크다운 원문 그대로(`**서울**`, `- `, 표) | 답만. 굵게는 빠지고, 목록은 ` * `, 표는 마크다운 표 |

- 사용자 결과(2026-10-29)와 같은 것: 시험 1(셋 다 입력창에서 기다림), 시험 2 ChatGPT, 시험 3(셋 다 링크만. ChatGPT 링크는 답 하나, Gemini는 질문·답 한 쌍).
- 조금 다른 것: Claude 시험 1에 '보낼 곳' 단계가 하나 더 있다. 사용자 폰은 '늘 새 세션으로'가 켜져 있었을 수 있다.
- 테스트 폰에서 못 본 것: Claude 시험 2를 웹에서 로그인한 채로(테스트 폰 Chrome은 claude.ai 로그인 화면으로 갔고 질문은 `returnTo`에 남았다. 로그인은 하지 않음), Claude 시험 3의 공개 링크 내용(공개는 하지 않음. 사용자는 질문 2개·답 2개가 모두 들어감을 봤고, 시트 문구와 맞다). ChatGPT 웹에 로그인하지 않았을 때 질문이 바로 보내지는 것도 사용자 시험에서만 봤다.
- 새로 안 것: Gemini는 주소로 질문을 넣을 수 없다. ChatGPT·Claude·Gemini 주소는 앱 링크로 확인돼 있어(`pm get-app-links`, verified) 앱이 깔려 있으면 앱에서 열린다. Claude 앱은 주소로 들어온 질문에만 경고를 붙인다(공유로 보내면 경고 없음).
- iOS는 아직 못 했다(Mac·iPhone이 생기면).

P2-4에 주는 뜻(답 받기는 2026-10-30 사용자 결정, 질문 넘기기는 안):

- **질문 넘기기:** ChatGPT는 주소(앱이 없으면 웹으로), Claude는 공유(경고 없음, 이어서 묻기도 됨), Gemini는 공유만. 셋 다 사용자가 보내기를 눌러야 간다.
- **답 받아 오기:** 공유는 셋 다 링크만이라 답 글을 받을 수 없다(Claude는 공개까지 해야 한다). 그래서 **복사 → Asksemble로 돌아와 붙여 넣기**가 길이다. 복사에는 질문이 없지만 앱이 질문을 알고 있다. 서식은 Claude만 원문 마크다운이고 ChatGPT·Gemini는 굵게가 빠지지만 목록·표는 남아 비교 판정에는 지장이 없다.
- **사용자 결정(2026-10-30): 답은 복사 → 붙여 넣기로 받는다.** 제품 방향 문서의 "답은 공유로 받아…"를 "복사해 붙여 넣는다"로 고쳤다(rev 79: 개요, 흐름 4단계, 연결 표, Phase 2 범위, 구조, 확인 목록. iOS 시험은 확인 목록에 남김).
- 시험 흔적: 각 앱에 시험 대화(ChatGPT 2개, Claude 1개, Gemini 2개)와 공개 링크 2개(ChatGPT·Gemini 첫 답)가 남았다. 지울지는 사용자가 정한다. ColorNote 시험 메모는 휴지통으로 옮겼다.

### P0-7 묻기의 웹 검색 (2026-10-30)

묻기에는 웹 검색을 기본으로 켠다(2절 모드 결정). 정답을 아는 질문("Rust 최신 안정판 버전과 확인한 주소", 정답 1.99.1, 2026-10-16 GitHub 릴리스)으로 AI마다 옵션을 바꿔 봤다. 머리말을 붙여 중립 시험 폴더에서, 권한을 푸는 옵션 없이 돌렸다. 모델은 '기본' 단계.

| AI | 실행 | 웹 도구 | 답 | 시간 |
| --- | --- | --- | --- | --- |
| Claude | `--tools ""`(지금 명령) | 없음 | "확인 못 함, 1.99(추정)" | 13.9초 |
| Claude | `--tools "WebSearch,WebFetch"` | **거절됨**(`permission_denials`) | 확인 못 함 | 13.2초 |
| Claude | `--restricted --tools "WebSearch,WebFetch"` | 거절됨 | 확인 못 함 | 17.6초 |
| Claude | `--tools "WebSearch,WebFetch" --allowedTools "WebSearch,WebFetch"` | WebFetch | 1.99.1 | 13.8~19.8초 |
| Claude | `--tools WebSearch --allowedTools WebSearch` | WebSearch 2번 | 1.99.1 + 'Sources:' 목록 | 29.7~42.8초 |
| Codex | 지금 명령 그대로 | `web_search`(기본으로 켜짐) | 1.99.1 | 11.2초 |
| Codex | `codex exec --search …` | — | 오류(`exec`에는 없는 옵션) | — |
| Codex | `codex --search exec …` | `web_search`(실시간) | 1.99.1 | 9.7~11.1초 |
| OpenCode | 지금 에이전트, npm `opencode.cmd`로 | — | "질문 본문이 비어 있다" | 9.7초 |
| OpenCode | 같은 에이전트, 실제 `opencode.exe`로 | webfetch 2번 | 1.99.1 | 22.8초 |
| OpenCode | 권한 `{"*": "deny", "webfetch": "allow", "websearch": "allow"}` | websearch 또는 webfetch | 1.99.1 | 8.4~25.6초 |

- **Claude:** `-p`에서는 WebSearch·WebFetch도 허용 규칙이 없으면 묻지 않고 거절된다(`--restricted`도 같다). 그래서 이 두 도구만 `--allowedTools`로 허용한다. `--tools`가 이 둘만 남기므로 파일·명령 도구는 아예 없다(웹을 켠 채 쓰기 시험 FAILED). WebSearch를 쓰면 도구 규칙 때문에 답 끝에 'Sources:' 목록이 붙는다. 앱은 이걸 출처로 보여 주고, 판정(P2-3)은 답 문장에서 뺀다.
- **Codex:** `--ignore-user-config`여도 웹 검색(캐시된 결과)이 기본으로 켜져 있다. `--search`(실시간)는 `exec` 앞에 붙이는 최상위 옵션이다. 읽기 전용은 그대로라 `--search`를 켠 쓰기 시험은 "blocked by policy"로 막혔다.
- **OpenCode는 실제 exe로 부른다.** npm이 만든 `opencode.cmd`를 거치면 cmd.exe가 줄바꿈 뒤를 버려 머리말 뒤의 질문이 사라졌다. cmd.exe는 질문 글 속 `%`·`"`·`&`도 해석할 수 있어 위험하다. 에이전트는 `.cmd`·`.bat`을 부르지 않고 실제 exe(`%APPDATA%\npm\node_modules\@opencode\cli\bin\opencode.exe`)를 찾아 부른다. Codex는 이미 exe다.
- **OpenCode P0-2 구멍:** 편집·셸만 막은 묻기 에이전트가 하위 에이전트(`subagent`, general)를 불러 파일을 썼다(바로 지움). 하위 에이전트는 전역 AGENTS.md도 읽어 '다음에 쓸 문장'까지 붙였다. 도구를 하나씩 막으니(`task` 포함) 세션·worktree 도구가 남았다. 권한을 `"*": "deny"`로 다 막고 webfetch·websearch만 허용하자 도구가 skill·webfetch·websearch만 남았고 쓰기 시험 4번 모두 FAILED.
- **웹 요청이 가는 곳:** OpenCode websearch는 검색 제공자를 골라 쓴다(이번엔 Firecrawl). 질문이 그 회사로 간다. OpenCode webfetch와 Claude WebFetch는 이 PC에서 그 주소로 직접 접속한다. Claude WebSearch와 Codex `web_search`는 각 회사 서버에서 검색한다. `docs/privacy.md`에 적었다.
- **최종 명령 셋을 동시에(웹 켬):** 웹이 필요 없는 질문(HTTP·HTTPS 차이)은 셋 다 도구를 쓰지 않고 9.9초(Claude 9.9, Codex 5.8, OpenCode 8.8). Rust 질문은 셋 다 1.99.1, 19.8초(Claude 19.8 WebFetch, Codex 9.7, OpenCode 14.0 webfetch+websearch).

**사용자 결정(2026-10-30, 추천대로):**

- Claude `--allowedTools "WebSearch,WebFetch"`는 '권한을 푸는 옵션 금지'에 걸리지 않는다. 권한 검사를 끄는 옵션(`--dangerously-skip-permissions`, `bypassPermissions`)이 아니라 웹 도구 둘에만 거는 허용 규칙이고, 다른 도구는 아예 없다. 제품 방향 문서 원칙 항목(rev 81)과 CLAUDE.md에 적었다.
- OpenCode는 websearch(외부 검색 제공자)와 webfetch를 켠다. `docs/privacy.md`에 묻기 항목으로 적었다.
- Codex는 실시간 검색(`--search`)을 쓴다(캐시와 시간 차이가 없고 정보가 최신).

### Phase 3 대비: Codex·OpenCode도 같은 구조가 되는가 (2026-10-29 확인)

Phase 3에서는 Codex·OpenCode 세션도 Claude처럼 목록·시작·재개·공식 앱으로 넘기기를 한다(제품 방향 문서 Phase 3 절). 묻기 세션을 가리는 분류(`ask` 배지)도 같은 방식으로 된다.

| | Claude(Phase 1) | Codex | OpenCode |
| --- | --- | --- | --- |
| 세션 기록과 폴더 | `~/.claude/projects/*/<uuid>.jsonl`, 줄마다 `cwd` | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`, 첫 줄 `session_meta`에 `cwd`·`id`·`source`·`originator`(이 PC 104개) | `opencode session list --format json` → `id`·`title`·`directory`·`created`·`updated`(폴더별, DB는 `opencode.db`) |
| 묻기 세션 가리기 | 폴더가 묻기 폴더면 `ask` | 같음(`session_meta.cwd`) | 같음(`directory`). 묻기 세션이 정말 묻기 폴더로 기록되는 것을 확인함 |
| 재개 | `claude --bg --resume` | `codex resume <id>`, `codex exec resume` | `opencode --session <id>`, `run --session` |
| 떠 있는 세션에 메시지 넣기 | 없음 | **`codex queue --thread <id> --message`** 가 있다 | 서버 API(`opencode serve`) |
| 공식 앱으로 넘기기 | Claude 앱(Remote Control, 검증됨) | `codex remote-control start`·`pair`(실험 기능, ChatGPT 앱) | `opencode pair --url <주소>`(웹·앱). 이 PC는 이미 serve 443 → OpenCode로 쓰고 있다 |

- 에이전트 쪽은 이미 이렇게 짜 두었다: `core.Adapter`(목록)와 선택 인터페이스(`Launcher` 열기·멈추기, `Previewer` 미리보기, Phase 2의 `Asker`), 세션 id `<agent>:<id>`와 `agent` 필드, AI별 capability, `badges`. Codex·OpenCode 어댑터를 `internal/adapter/codex`, `internal/adapter/opencode`로 더하면 API와 앱은 그대로 쓴다(원칙 1~3).
- 주의할 것: Codex 원격은 아직 실험 기능이다. OpenCode `pair`는 `--remote`(인터넷에 엶)를 절대 쓰지 않고 Tailscale 주소(`--url`)만 쓴다. Codex 상태 판정은 `codex agents`가 사람용 화면뿐이라 기록과 app-server로 따로 확인해야 한다. 이런 확인은 Phase 3 시작 전에 Claude의 구현 전 테스트처럼 한다.

### Phase 4 대비: 일 나눠 맡기기 검토 (2026-10-29)

제품 방향 문서의 Phase 4(큰 일 하나를 여러 AI에게 나눠 맡기고 사람이 확인)를 지금까지 알게 된 것과 맞춰 봤다.

| 구성 | 되는가 | 근거와 남은 것 |
| --- | --- | --- |
| 조사 나눠 맡기기(일반) | 됨 | Phase 2 묻기(P2-1)를 여러 번 쓴다. 다만 조사는 웹을 읽어야 해서 묻기의 "도구 모두 끔"과 다르다. 작업 종류별 도구 정책이 필요하다: 답만(도구 끔) / 조사(웹 읽기만: Claude `--tools "WebSearch,WebFetch"` 같은 식, P0-7) / 코딩(작업 공간 안 쓰기) |
| 코딩 작업 나누기(개발) | 됨, Phase 3에 기댐 | 파일을 고치면 권한 요청이 생기는데 `-p`는 사람에게 물을 수 없어 거절한다. 그래서 코딩 작업은 사람이 승인할 통로가 있는 방식으로 돌아야 한다: Claude는 `--bg --rc`(Claude 앱에서 승인, Phase 1에서 됨), Codex·OpenCode는 Phase 3의 공식 앱 연결이나 Chat UI(승인 버튼). Phase 4가 Phase 3 뒤인 순서가 맞다 |
| 작업 공간 분리(git worktree) | 됨, 범위가 넓어짐 | Claude에는 `-w`(worktree)가 있다. 다른 AI까지 하려면 에이전트가 직접 `git worktree add`를 해야 하는데, 지금 에이전트는 정해진 `claude` 명령만 실행한다(N2). git 명령 몇 개를 허용 목록에 더하는 설계 변경이 필요하다 |
| 동시 실행·사용량 제한 | 됨 | 셋을 동시에 돌려도 각자 4~16초였다. 사용량은 응답에 있다: Claude `total_cost_usd`(구독은 환산값), Codex `turn.completed.usage`(토큰), OpenCode `stats`. 이걸로 코드가 한도를 센다 |
| 판단(배정·위험도·범위·결과 검증) | 됨 | 설계의 Choice·Score·Noul 꼴이 TypeSafe의 지금 기본 단위와 맞는다(스킬로 확인). 기본은 구독 AI, Jev는 옵션 그대로. 흐름과 기준값은 코드가 갖고 AI는 판단 하나만 |
| 알림 | **설계끼리 어긋남** | Phase 4 절은 "PC 에이전트가 Expo 푸시 API를 직접 부른다"고 적었는데, 10-29에 F9를 미루며 "에이전트는 푸시를 보내지 않고 폰이 확인한다"(Phase 1 설계 rev 65)로 정했다. Phase 4는 오래 걸리는 일이라 알림이 더 중요하다. Phase 4 전에 다시 정한다 |
| 결과 모으기·합치기 | 됨 | 조사는 Phase 2의 같은 말·다른 말 판정을 다시 쓴다. 코딩은 worktree마다 diff를 모으고 합치기는 사람이 한다 |

**큰 그림에서 정해야 할 원칙 하나: N1(에이전트는 밖으로 나가는 요청이 없다).** 지금은 지켜지지만 앞으로 세 군데가 부딪친다.
- Expo 푸시(알림, Phase 4)
- Jev 같은 판정 전용 모델(옵션, Phase 2·4). TypeSafe 지침상 API 키는 폰이 아닌 쪽(여기서는 PC 에이전트)에 두어야 하므로, 쓰려면 에이전트가 TypeSafe로 요청을 보낸다.
- Codex·OpenCode 공식 앱 연결(Phase 3). 이건 각 CLI가 스스로 나가는 것이라 에이전트의 요청은 아니다.

제안: N1을 "기본으로는 나가는 요청이 없고, 사용자가 켠 연동(알림, 판정 모델)만 그 서비스로 나간다. 무엇이 나가는지 설정에 보인다"로 다듬는다. Phase 4나 Jev를 시작하기 전에 정하고 원본 설계를 고친다.

**그 밖에 지킬 것**
- Codex 사용자 설정의 자동 승인(`approvals_reviewer = "auto_review"`)은 Phase 4 코딩에서도 따르지 않는다. 설계 표의 "자동으로 허용하지 않는다"와 같은 뜻이다. 사용자 설정을 빼거나 승인 정책을 직접 넘긴다.
- Codex의 `workspace-write`(작업 폴더 안 쓰기 허용) 샌드박스를 '권한을 푸는 옵션'으로 볼지 정해야 한다. 샌드박스는 그대로이고 쓰기 범위만 넓히는 것이라 worktree 안에서는 허용하는 쪽을 제안한다. Phase 4 전에 정한다.
- PC가 없는 일반 사용자의 긴 조사는 클라우드 PC(사업 트랙)와 묶인다. 설계 그대로다.

**결론:** Phase 4는 Phase 2(묻기)·Phase 3(다른 AI 세션, 승인 통로) 위에 쌓는 구조라 지금 순서로 갈 수 있다. 시작 전에 정할 것은 N1 다듬기, 알림 방식, git worktree 허용, `workspace-write` 해석 네 가지다.

### 정리: AI별 묻기 명령 (P0-2·P0-7·P0-8·P0-9, P2-1 시험 반영)

질문(머리말 + 빈 줄 + 질문)은 셋 다 **stdin**으로 넘기고 인자에 넣지 않는다(P2-1). 그래서 질문이 `-`로 시작해도 옵션으로 읽히지 않고, 줄바꿈·`%`·`"`·`&`도 그대로 가며, 명령 줄 길이 제한도 없다.

| AI | 첫 질문 | 후속 질문 | 막는 것 |
| --- | --- | --- | --- |
| Claude | `claude -p --output-format json --model M --effort E --tools "WebSearch,WebFetch" --allowedTools "WebSearch,WebFetch" --strict-mcp-config` | `+ --resume <session_id>` | 웹 도구 둘만 있고 둘만 허용, **MCP 서버를 싣지 않음**(claude.ai 커넥터 포함), 중립 폴더(CLAUDE.md 없음) |
| Codex | `codex --search exec -s read-only --ignore-user-config --skip-git-repo-check --json -m M -c model_reasoning_effort="E" -` | `codex --search exec -s read-only resume --ignore-user-config --skip-git-repo-check --json -m M -c model_reasoning_effort="E" -c sandbox_mode="read-only" <thread_id> -` | 읽기 전용 + 사용자 설정(자동 승인·MCP) 뺌, 머리말(전역 AGENTS.md) |
| OpenCode | `opencode.exe run --standalone --format json --agent asksemble-ask -m M#변형`(npm `.cmd`가 아닌 실제 exe), 환경 `PWD`=묻기 폴더, `OPENCODE_CONFIG_CONTENT`=묻기 에이전트 | 먼저 `session list --format json --standalone`(같은 환경)로 묻기 폴더의 세션인지 확인, 그다음 `+ --session <sessionID>` | 묻기 에이전트(권한 `"*": "deny"`에 webfetch·websearch만 allow, 짧은 지시) |

- 셋 모두 셸 없이 인자 배열로, 묻기 폴더에서 실행한다. 머리말은 셋에 똑같이 붙인다.
- 에이전트는 만든 인자를 한 번 더 검사해, 위 모양과 조금이라도 다르면(다른 옵션, 다른 도구, 질문 인자) 실행하지 않는다(어댑터마다 `checkAskArgs`·`checkArgs`).
- Windows에서는 `.cmd`·`.bat`을 거치지 않고 실제 exe를 부른다(P0-7: 줄바꿈 뒤가 잘리고, cmd.exe가 질문 글을 명령으로 해석할 위험). PATH에서 `이름.exe`를 찾고, npm `.cmd`만 있으면 그 옆 `node_modules` 안의 exe를 찾는다(`codexPath`·`opencodePath` 설정으로 바꿈).

### P2-1 만든 것 (2026-10-30)

**API.** `POST /v1/runs`는 바로 202와 `runId`를 주고, 폰은 `GET /v1/runs/{runId}`로 묻는다(작업 id와 같은 방식, 1초 간격). AI마다 답이 끝나는 대로 보이므로 느린 AI가 다른 답을 막지 않는다.

```
POST /v1/runs
{"mode": "ask", "prompt": "질문", "tier": "quick|standard|deep",
 "agents": [{"agent": "claude"}, {"agent": "codex", "model": "...", "effort": "high"},
            {"agent": "opencode", "session": "opencode:ses_…"}],
 "clientRunId": "폰이 정한 id(선택)"}
→ 202 / 200 {"runId", "mode", "tier", "status": "running|done|failed", "startedAt", "finishedAt",
             "pollAfterMs", "parts": [{"agent", "status", "session": "<agent>:<id>", "model",
             "answer", "ms", "inputTokens", "outputTokens", "error": {"code", "message"}}]}
```

- `agents`를 비우면 이 PC에서 물을 수 있는 AI 모두. `session`이 있으면 그 대화를 이어 간다(후속 질문 A안). 같은 AI는 한 번만.
- `mode`가 `work`·`code`면 501 `mode_not_available`(자리만, `/v1`은 더하기만). 질문은 32KB까지, 한 번에 4개 묻기까지(429 `too_many_runs`), AI 하나에 3분, 끝난 묻기는 30분 동안 읽을 수 있다. 같은 `clientRunId`를 다시 보내면 새로 묻지 않고 처음 것을 준다(요청이 끊겨 폰이 다시 보낼 때).
- 오류 코드: `ask_failed`(AI가 낸 오류, `message`는 그 AI의 글), `ask_timeout`, `ask_empty`, `bad_model`, `session_not_found`(이어 갈 세션이 없음), `session_busy`(그 대화가 아직 답하는 중이거나 원격 세션으로 열려 있음: 요청 전체는 409, Claude 어댑터 안에서는 그 AI의 부분 오류), `agent_unavailable`.
- 한 세션에는 후속 질문이 하나씩만 돈다. 두 개가 동시에 이어 쓰면 대화가 갈라진다(Phase 1의 '돌고 있는 세션은 재개하지 않는다'와 같은 규칙). Claude는 폰에서 열기·멈추기 중이거나 원격·터미널 세션으로 떠 있어도 묻기 후속을 하지 않는다.
- 질문과 답은 로그에 남기지 않는다. 로그에는 모드·단계·AI 이름과 AI별 상태·시간·오류 코드만.
- `/v1/info`의 `agents`에 Codex·OpenCode가 더해졌다(capability `ask`만, 세션 목록은 Phase 3). 묻기가 되는 AI에는 `ask.tiers`(단계별 모델)가 붙는다(P2-2 칩 시트에 씀).
- 묻기 세션은 목록에서 `ask` 배지, 묻기 폴더는 '기록 폴더'(새 세션 폴더)에 나오지 않는다.

**에이전트 구조.** `core.Asker`(AI 하나에 한 번 묻기), `internal/runs`(여러 AI 동시, 부분 결과, 시간 제한, 에이전트가 멈추면 묻기도 정리), `internal/proc`(셸 없이 exe 실행, 질문은 stdin, Windows에서는 job object로 묶어 시간 초과·종료 때 하위 프로세스까지 끔), 어댑터 `claude/ask.go`, `internal/adapter/codex`, `internal/adapter/opencode`. 설정(`agent.json`)에 `ask.folder`(기본 `%LOCALAPPDATA%\Asksemble\ask`), `ask.tiers`(AI·단계별 모델 바꾸기), `codexPath`, `opencodePath`. PC에서 바로 시험하는 `asksemble-agent ask [-agents …] [-tier …] [-follow ID,…] 질문`, `info`는 세 AI를 모두 보이고 `check`도 Codex·OpenCode를 본다.

**앱.** 연결 진단이 AI마다 이름을 쓴다(Claude Code·Codex·OpenCode). 묻기만 하는 AI가 PC에 없으면 실패(✗)가 아닌 안내(·)로 보이고, 세션 목록 위에는 세션 목록을 가진 AI의 알림만 나온다. `ask` 배지 이름(묻기·Ask). 묻기 탭 자체는 P2-2. 묻기만 하는 AI가 있어도 동작하지 않으면(npm 스크립트뿐, 버전 확인 실패) 그 까닭을 안내로 보인다. 폰에 깔린 예전 APK는 진단에서 셋 모두를 'Claude Code x.y.z'로 적고, PC에 없는 AI는 실패(✗)로, 그 알림 코드는 세션 목록 위에 코드 그대로 보인다. 이 PC는 셋 다 있어 이름만 틀리고, 새로 빌드하면 고쳐진다.

**시험으로 새로 안 것(2026-10-30, 중립 시험 폴더, 권한 푸는 옵션 없이).**

| AI | 알게 된 것 | 반영 |
| --- | --- | --- |
| 셋 다 | 질문을 stdin으로 받는다(머리말의 줄바꿈·한국어 그대로) | 인자에 질문을 넣지 않음 |
| Claude | `--strict-mcp-config` 없이는 claude.ai 커넥터의 MCP 도구(Claude Docs 만들기·고치기·지우기 등 8개)가 묻기에 들어온다. 지금은 허용 규칙이 없어 막히지만 사용자가 규칙을 더하면 열린다 | `--strict-mcp-config`(MCP를 싣지 않음, 도구를 줄이는 옵션)를 더함. 도구는 WebFetch·WebSearch 둘만 남음을 확인 |
| Claude | 사용자 설정의 훅(Orca)이 묻기마다 모든 이벤트(PermissionRequest 포함)에서 돈다 | 도구가 둘뿐이라 훅이 승인해도 쓸 도구가 없다. 개인정보 문서에 '설정한 확장에도 전해질 수 있음'을 적음 |
| Claude | 오류여도 `subtype`은 `success`다. `is_error`·`api_error_status`·stderr의 `[claude-code:unrecognized_model]`로 가른다. 이어 간 세션의 `total_cost_usd`·`modelUsage`는 세션 누적이다 | 이번 실행의 `usage`만 씀. 모델 이름은 `modelUsage` 키(`claude-sonnet-5-5`) |
| Codex | `--search`는 `exec` 앞에만, `-s`는 `exec` 뒤·`resume` 앞에만 된다. `resume` 뒤에는 `-c sandbox_mode="read-only"`로 한 번 더 | 후속 명령 고정 |
| Codex | 한 턴에 `agent_message`가 여럿일 수 있고 앞의 것은 "확인하겠습니다" 같은 머리글이다. `item.type: error`는 경고일 뿐이다. 실패는 `turn.failed`이고 메시지 안에 JSON이 한 번 더 들었다. 없는 thread는 stderr에 `no rollout found`. 모델 이름은 출력에 없다 | 마지막 `agent_message`가 답, 넘긴 모델 이름을 씀 |
| OpenCode | `--session`에 없는 id를 주면 실패하지 않고 **그 id로 빈 세션을 새로 만들어** 앞 대화가 조용히 사라진다 | 후속 질문 전에 묻기 폴더의 세션 목록에서 확인(2~3초 더 걸림). 다른 폴더의 세션도 이어 가지 않는다 |
| OpenCode | 마지막 `step_finish`가 안 나올 때가 있다. 모델 이름은 출력에 없다. 오류는 stdout의 `type: error`(모르는 모델은 `provider.no-route`) | 프로세스가 끝나면 답이 끝난 것으로 봄. 마지막 단계의 글이 답 |
| OpenCode | `"*": "deny"`여도 `skill` 도구는 목록에 남는다. `"skill": "deny"`, 에이전트 `tools: {skill: false}`로도 안 빠졌다 | 그대로 둔다. 스킬 글을 읽기만 하는 도구이고 호출은 `"*": "deny"` 아래에 있다 |

- 시간(이 PC, '빠르게', 셋 동시): `ask` 명령 첫 질문 12.5초·후속 13.5초, LT-05 첫 질문 10.7초·후속 13.7초. OpenCode 후속은 세션 확인 몫이 더해져 11~13초.
- 시험 흔적: 실제 묻기 폴더 `ask`에 `ask` 명령 시험 세션이 AI마다 하나씩, LT-05는 이제 `ask\live`에 남긴다(묻기 폴더 안이라 `ask` 배지가 붙고 새 세션 폴더에 안 나옴). 그 밖의 빈 시험 폴더 `ask-test`(10-29)·`ask-config`(P0-9)·`ask-probe`(P2-1 시험)·`ask-live`(LT-05 첫 실행)는 10-30 사용자 요청으로 지웠다. 폴더가 없으니 '기록 폴더'에 안 나오고, 그 폴더에서 한 시험 대화는 세션 목록에 '폴더 없음'으로 남는다.
- 검토(10-30, 세 갈래 검토 + 찾은 것마다 반박 두 번): 24건 중 11건을 고쳤다. 버전 확인이 끊긴 요청을 실패로 기억함(R-19), 출력 상한이 동작하지 않음(R-18, Phase 1 실행도), 같은 세션에 후속 질문 둘(R-20), `proc` 테스트 없음, 앱 진단의 '있지만 동작 안 함', OpenCode 세션 확인 테스트, `ask` 명령이 답이 없을 때 0으로 끝남, 그리고 반박됐지만 맞았던 것(요청 크기 여유, 묻기 폴더를 못 정하면 에이전트 전체가 안 뜸, LT-05 폴더, `test-live.ps1`이 영구 환경 변수까지 지움, 예전 APK 설명, Codex npm 대상 CPU).
- 남은 것: 묻기 멈추기(P2-2에서는 넣지 않음), Codex·OpenCode 세션 목록(Phase 3). 머리말 언어는 10-30에 영어 머리말로 해결(P0-9 절).

### 후속 질문 시간과 `--bg` 비교 (2026-10-29, P0-8 일부)

사용자 질문: 묻기도 `--bg`로 열어 두고 시간 제한을 두면 빠르지 않을까. 기본 단계 모델로, 첫 질문 "HTTP와 HTTPS의 차이를 두 문장으로" → 후속 "방금 답에서 TLS가 하는 일을 한 문장으로 더"를 쟀다.

| 방식 | 첫 질문 | 후속 질문 | 비고 |
| --- | --- | --- | --- |
| Claude `-p` → `-p --resume <id>` | 7.5초 | **7.3초** | 중립 폴더 |
| Claude `--bg`(답이 기록에 써질 때까지) → 멈춤 → `--bg --resume <id> <후속>` | 12.9초 | 16.2초 + 멈춤 1.6초 | `rc-test`(`--bg`는 신뢰한 폴더에서만 됨). 같은 기록에 이어 써짐(복사본 없음) |
| Codex `exec` → `exec resume <thread_id>` | 8.8초 | **7.7초** | 중립 폴더 |
| OpenCode `run` → `run --session <id>` | 4.7초 | **3.8초** | 중립 폴더 |

- 같은 `rc-test`에서 첫 질문만 따로 두 번 재도 `-p` 11.4·8.8초, `--bg` 15.1·13.2초였다. `--bg`는 서비스가 세션을 띄우고 원격 연결까지 붙이는 몫이 더해진다.
- 떠 있는 `--bg` 세션에 질문을 넣고 답만 받는 CLI 명령이 없다(`attach`는 사람이 쓰는 터미널 화면, `logs`는 출력 보기). 그래서 `--bg`의 후속 질문은 멈췄다가 다시 띄워야 하고, 그만큼 느리다.
- **결론: 묻기는 `-p`(Codex `exec`, OpenCode `run`)와 resume으로 간다.** 후속 질문도 셋 다 4~8초라 A안(AI마다 이어 가기)이 그대로 된다. 시험으로 만든 `--bg` 세션은 멈추고 지웠다.

### 앱에서 고르는 화면 (P2-2 초안)

여러 AI 채팅 앱에서 흔한 방식을 따른다: 입력창 근처의 모델 고르기, 여러 모델 앱의 AI별 켜고 끄기, 답을 AI별 카드로 나란히.

- 입력창 아래 한 줄: **빠르게 · 기본 · 깊게** 칩(기본이 선택됨). 고른 단계가 켜 둔 AI 모두에 적용된다.
- 그 위 한 줄: AI 칩(Claude · Codex · OpenCode)으로 이번 질문에 물을 AI를 켜고 끈다. PC에 없는 AI는 흐리게.
- AI 칩을 길게 누르면 그 AI만 모델을 따로 고르는 시트(단계 표의 모델 + PC가 알려 주는 다른 모델).
- 답은 AI별 카드로 나오고 카드마다 모델 이름과 걸린 시간을 작게 보인다. 고른 단계와 모델은 폰에 기억해 다음 질문의 기본으로 쓴다.

### P2-2 만든 것 (2026-10-30)

- **두 입구를 아래 탭으로**: '한 번에 / All at once'(왼쪽)와 '내 PC / My PCs'. 주소는 그대로(`/`는 내 PC, `/ask`는 묻기)라 앱은 예전처럼 '내 PC'로 열리고 E2E 흐름의 주소도 그대로다. 설정은 두 탭 머리글에. 키보드가 뜨면 탭 막대를 숨긴다.
- **묻기 화면**(`src/app/(tabs)/ask.tsx`): 대화가 위, 입력이 맨 아래(`FormScroll`로 키보드 위에 보이게). 입력 영역은 위에서부터 AI 칩(켜고 끄기, PC에 없거나 동작 안 하는 AI는 흐리게, **길게 누르면 그 AI만 모델 고르기**: 단계대로 또는 PC가 알려 준 단계 모델 중 하나) → 빠르게·기본·깊게 → 질문 칸 → 묻기 버튼 → 새 질문. 초안은 단계 칩이 질문 칸 아래였으나, 테스트 폰(S10)에서 키보드가 버튼을 가려 칸 위로 옮겼다(질문 칸 바로 아래가 버튼이어야 키보드 위에 같이 보임).
- **답 카드**(`src/ui/AnswerCard.tsx`): AI 이름, 걸린 시간·모델(시간이 먼저: 긴 모델 이름이 잘려도 시간은 보이게), 답(길게 눌러 고르기 가능), 복사, '이 AI에게만', Claude 카드에만 'Claude 앱에서 이어서 하기'(Phase 1 열기 규칙 그대로). 실패한 AI는 까닭(AI의 메시지 포함)을 보이고 다른 AI 답은 그대로 온다.
- **이어서 묻기**: 기본은 켜 둔 AI 모두, '이 AI에게만'을 누르면 그 AI만('Codex에게만 ✕'로 표시, 누르면 모두로). AI마다 자기 마지막 세션을 이어 간다(A안). '새 질문'은 새 대화. 묻는 동안에는 다음 질문을 막는다(한 세션에 후속 하나, R-20과 같은 규칙).
- **보내기와 받기**: 질문마다 폰이 id(`clientRunId`)를 정해, 요청이 끊기면 같은 id로 다시 보낸다(두 번 묻지 않음). 답은 1초마다 묻고, 잠깐 끊겨도 30번까지 다시 묻는다. 그래도 놓치면 '다시 받기'(같은 묻기를 이어 받음).
- **PC 고르기**: PC가 둘 이상이면 위에 칩. 고른 PC, 단계, 끈 AI, AI별 모델은 폰에 기억한다(`src/ask/prefs.ts`). 설정에 '묻기 기본 단계'(묻기 화면에서 고른 것과 같은 값).
- **내 PC 세션 목록**: '묻기 대화' 칩. 다른 칩(전체 포함)은 묻기 세션을 빼고, 이 칩은 묻기 세션만 보인다.
- **대화는 아직 메모리에만** 있다(앱을 끄면 화면에서 사라지고 PC의 각 AI 기록에는 남음). 폰 기록은 P2-5. 답은 글 그대로 보인다(마크다운 꾸밈 없음).

테스트 폰(Galaxy S10)에서 실제 에이전트(P2-1 배포본)와 세 AI로 본 것(10-30):

- 첫 질문(빠르게): Claude 11.8초, OpenCode 11.9초에 답이 카드로 하나씩 들어왔다. 후속 질문(모두에게)도 Claude·OpenCode가 각자 앞 대화를 이어 답했다(10.8·14.4초).
- **Codex는 사용량 한도**에 걸려 "You've hit your usage limit … try again at 1:22 PM"을 냈다(오늘 시험이 많았음). 카드에 그 글이 그대로 보여 실패 표시도 실제로 확인됐다.
- **'Claude 앱에서 이어서 하기'는 묻기 폴더를 신뢰해야 된다.** Phase 1 열기는 `--bg`라 신뢰한 폴더에서만 되는데, 묻기 폴더(`%LOCALAPPDATA%\Asksemble\ask`)는 신뢰하지 않은 폴더다. 앱은 `workspace_not_trusted`를 받아 "PC 터미널에서 그 폴더로 가 claude를 실행하고 신뢰를 허용하세요"(폴더 경로 포함)라고 안내한다. 신뢰 허용은 사람이 하는 일이라(R-10과 같은 원칙) 대신 하지 않았다. 설치 흐름이나 `check`에 넣을지 정해야 한다.
- **한국어 머리말이 답 언어를 끈다.** 영어 첫 질문에는 영어로 답했지만, 짧은 영어 후속("Now say it in one short sentence.")에 Claude가 한국어로 답했다. 시험으로 원인을 확인하고 머리말을 영어로 바꿨다(4절 P0-9 '머리말을 영어로 바꿈', 사용자 요청 10-30).
- 테스트 폰에서 한국어를 adb로 입력하면 들어가지 않아(agent-device `fill`) 폰 시험 질문은 영어로 했다. 한국어 질문은 ST(`ask-test.tsx`)가 본다.

## 5. 단계 (초안)

| 단계 | 할 일 |
| --- | --- |
| P2-1 | 에이전트 `core.Asker`와 AI별 ask 어댑터(claude·codex·opencode, 4절 '정리'의 명령), `POST /v1/runs`(mode `ask`, 작업 id, 여러 AI 동시. 2절 결정), capability `ask`, 묻기 세션 `ask` 배지와 묻기 폴더를 기록 폴더에서 빼기. **끝(2026-10-30), 4절 'P2-1 만든 것'** |
| P2-2 | 앱 '한 번에 묻기' 탭(원칙 9의 숨긴 자리를 켬): 질문 입력, 빠르게·기본·깊게와 AI 고르기, 답 카드, 'Claude 앱에서 이어서 하기'. Phase 1 목록의 '묻기 대화' 칩. **끝(2026-10-30), 4절 'P2-2 만든 것'** |
| P2-3 | 비교 판정: 같은 말·다른 말·한 AI만 한 말·확인 필요. 기본은 구독 AI(PC), Jev는 옵션 |
| P2-4 | 공식 앱으로 넘기기: 답은 복사 → 붙여 넣기(2026-10-30 결정). 질문은 안: ChatGPT 주소, Claude·Gemini 공유(4절 P0-4) |
| P2-5 | 기록은 폰에만, 판정 검증(P0-6) |

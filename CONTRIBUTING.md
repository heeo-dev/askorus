# Contributing

한국어는 [아래](#기여하기-한국어)에 있습니다.

Askorus is maintained by one person in spare time. Bug reports and ideas help the most; code is welcome too, after a word in an issue.

## Reporting a bug or an idea

- Open an [issue](https://github.com/heeo-dev/askorus/issues/new/choose). English or Korean, either is fine.
- For a bug, say what you did, what you expected and what happened, with the system (Windows, macOS, Linux), the agent's version (`askorus-agent version`) and the output of `askorus-agent check`.
- Issues are public. Take your account, PC name, addresses and folder paths out of what you paste, and never post a pairing link or code.
- A security problem goes in private: see [SECURITY.md](SECURITY.md).

## Changing the code

Please open an issue first and say what you want to change. The design is written down ([docs/phase1-plan.md](docs/phase1-plan.md), [docs/phase2-plan.md](docs/phase2-plan.md), mostly in Korean), and a change that goes against it is talked through before it is built.

Build:

```sh
cd agent && go build ./cmd/askorus-agent     # the PC agent, Go 1.27
cd app && npm ci                             # the phone app, Node 24
```

Test before you send a change:

| Where | Run | What it covers |
|---|---|---|
| Windows | `powershell -File scripts\test-all.ps1` | Everything: the agent's tests, the install script, the app's type check and tests. It must end with `ALL PASSED`. |
| macOS, Linux | `cd agent && go vet ./... && go test ./...` | The agent, with the pretend AI programs the tests bring along |
| macOS, Linux | `bash scripts/test-install.sh` and `bash scripts/test-unix.sh` | The install script, and start, stop, path and logon start for real, inside a temp folder |
| Any | `cd app && npx tsc --noEmit && npx jest` | The app |

A pull request also runs the agent's tests on GitHub's Linux and macOS machines. [docs/testing.md](docs/testing.md) has the levels and the list of past bugs with their tests.

What every change keeps to (the full list is in [CLAUDE.md](CLAUDE.md), which people and AI coding tools both follow here):

- The agent runs the AI programs without a shell, with fixed arguments only, and never with an option that turns their permission checks off.
- Nothing reads or moves an AI program's sign-in data.
- No secrets and no personal values (addresses, device names, home folder paths, mail) in code, tests, issues or commits.
- Screen text comes in English and Korean together.
- A new feature brings its tests; a bug fix brings a test that fails without it and a line in the table of [docs/testing.md](docs/testing.md).

By sending a change you agree that it is licensed under [Apache-2.0](LICENSE), like the rest.

---

## 기여하기 (한국어)

Askorus는 한 사람이 남는 시간에 돌보는 프로젝트입니다. 버그 신고와 제안이 가장 큰 도움이 됩니다. 코드도 환영하지만, 먼저 이슈에서 이야기해 주세요.

### 버그나 제안 알리기

- [이슈](https://github.com/heeo-dev/askorus/issues/new/choose)를 열어 주세요. 한국어, 영어 모두 좋습니다.
- 버그는 무엇을 했고, 무엇을 기대했고, 무슨 일이 일어났는지 적어 주세요. 운영체제(Windows, macOS, Linux), 에이전트 판(`askorus-agent version`), `askorus-agent check` 결과가 있으면 좋습니다.
- 이슈는 공개됩니다. 붙여 넣는 글에서 계정, PC 이름, 주소, 폴더 경로는 지워 주세요. 짝 맺기 링크와 코드는 올리지 마세요.
- 보안 문제는 비공개로 알려 주세요: [SECURITY.md](SECURITY.md).

### 코드 고치기

먼저 이슈를 열어 무엇을 바꾸려는지 알려 주세요. 설계가 문서로 있고([docs/phase1-plan.md](docs/phase1-plan.md), [docs/phase2-plan.md](docs/phase2-plan.md)), 설계와 다르게 가는 변경은 만들기 전에 이야기합니다.

빌드:

```sh
cd agent && go build ./cmd/askorus-agent     # PC 에이전트, Go 1.27
cd app && npm ci                             # 폰 앱, Node 24
```

변경을 보내기 전에 돌릴 테스트:

| 어디서 | 실행 | 무엇을 보나 |
|---|---|---|
| Windows | `powershell -File scripts\test-all.ps1` | 전부: 에이전트 테스트, 설치 스크립트, 앱의 타입 검사와 테스트. `ALL PASSED`로 끝나야 합니다. |
| macOS, Linux | `cd agent && go vet ./... && go test ./...` | 에이전트(테스트가 가져오는 가짜 AI 프로그램으로) |
| macOS, Linux | `bash scripts/test-install.sh`, `bash scripts/test-unix.sh` | 설치 스크립트, 그리고 켜기·끄기·PATH·로그인할 때 켜기를 임시 폴더 안에서 실제로 |
| 어디서나 | `cd app && npx tsc --noEmit && npx jest` | 앱 |

PR을 올리면 GitHub의 Linux와 macOS에서도 에이전트 테스트가 돕니다. 단계와 지난 버그의 테스트 목록은 [docs/testing.md](docs/testing.md)에 있습니다.

모든 변경이 지키는 것(전체 목록은 사람과 AI 코딩 도구가 함께 따르는 [CLAUDE.md](CLAUDE.md)에 있습니다):

- 에이전트는 AI 프로그램을 셸 없이, 정해진 인자로만 실행하고, 권한 확인을 끄는 옵션은 넘기지 않습니다.
- 어떤 AI 프로그램의 로그인 정보도 읽거나 옮기지 않습니다.
- 비밀값과 개인 값(주소, 기기 이름, 사용자 폴더 경로, 메일)은 코드, 테스트, 이슈, 커밋에 넣지 않습니다.
- 화면 문구는 한국어와 영어를 같이 넣습니다.
- 기능을 더하면 테스트를 같이 더하고, 버그를 고치면 그 버그를 잡는 테스트와 [docs/testing.md](docs/testing.md) 표의 한 줄을 더합니다.

변경을 보내면 그 내용이 나머지와 같이 [Apache-2.0](LICENSE)으로 배포되는 데 동의하는 것입니다.

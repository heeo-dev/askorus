<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/lockup/askorus-lockup-dark-transparent.svg">
    <img alt="Askorus" src="docs/brand/lockup/askorus-lockup-light-transparent.svg" width="420">
  </picture>
</p>

<h3 align="center">질문은 내가, 토론은 AI들이.</h3>
<p align="center">여러 AI의 답, 토론을 거쳐 더 나은 하나로.</p>

<p align="center"><a href="README.md">English</a></p>

---

Askorus(애스코러스)는 폰에서 질문 하나를 여러 AI에게 한 번에 묻는 앱입니다. 물어본 AI 중 하나가 사회자로 모든 답을 읽고 종합 답변을 쓰며, 의견이 같은 곳과 다른 곳을 보여 줍니다. 원하면 의견이 다른 곳을 AI끼리 토론하게 할 수 있습니다. 질문은 내 PC가 이미 쓰고 있는 공식 AI 프로그램으로 묻기 때문에 중간에 우리 서버가 없습니다.

개발자는 같은 앱에서 내 PC의 Claude Code 세션을 보고 새로 시작할 수 있고, 세션을 열면 Claude 앱이나 브라우저에서 이어 씁니다.

<p align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/ko/1-ask-dark.png"><img src="docs/screenshots/ko/1-ask.png" width="160" alt="질문하기 화면: 질문 하나와 그 질문을 받을 AI들"></picture>
  <picture><source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/ko/2-combined-dark.png"><img src="docs/screenshots/ko/2-combined.png" width="160" alt="종합 답변: 경주와 전주 중 AI마다 고른 곳"></picture>
  <picture><source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/ko/4-debating-dark.png"><img src="docs/screenshots/ko/4-debating.png" width="160" alt="토론: 사회자가 AI에게 차례로 묻고 AI가 답하는 중"></picture>
  <picture><source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/ko/5-result-dark.png"><img src="docs/screenshots/ko/5-result.png" width="160" alt="토론 결과: 목적에 따라 경주와 전주 중 어디가 맞는지"></picture>
</p>

<p align="center">
  <img src="docs/screenshots/ko/demo.gif" width="320" alt="실제로 돌린 화면을 빠르게 감은 것: 질문 하나가 Claude, Codex, OpenCode에 가고, 사회자가 종합 답변을 쓰고, 의견이 다른 곳을 AI끼리 토론해 쟁점마다 결과가 나옵니다">
</p>

**상태:** 개발 중입니다. 안드로이드 앱과 PC 에이전트(Windows, macOS, Linux)가 처음부터 끝까지 동작합니다. 에이전트는 GitHub 배포판에 있고 [한 줄로 설치](#pc-에이전트-설치)합니다. 앱은 아직 Google Play에 없어서, 지금은 배포판의 [미리보기 APK를 설치](#안드로이드-앱-설치)하거나 [소스에서 빌드](#소스에서-빌드)합니다.

## 이렇게 동작해요

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/how-it-works-ko-dark.svg">
  <img src="docs/images/how-it-works-ko-light.svg" width="640" alt="Askorus가 동작하는 네 단계. 1. 한 번 묻기: 폰에서 질문 하나를 켜 둔 AI 모두에게 보냅니다. 2. AI마다 답하기: Claude Code, Codex, OpenCode가 내 PC에서 각자 답합니다. 3. 사회자가 종합: 종합 답변에 의견이 같은 곳과 다른 곳을 함께 보여 줍니다. 4. 다른 곳은 토론: AI마다 생각을 유지하거나 바꾸고, 쟁점마다 결과가 나옵니다.">
</picture>

사회자는 물어본 AI 중 하나이고, 토론은 원할 때만 합니다.

## 기능

- 여러 AI에게 한 번에 묻기, 모두에게·한 AI에게·사회자에게만 이어서 묻기
- 종합 답변과 같은 곳·다른 곳 표시
- AI끼리 토론, 쟁점마다 결과(합의, 상황에 따라 다름)
- AI마다 모델과 생각 깊이 고르기
- 답을 공유하거나 신고하기, PC를 준비하기 전에 예시 대화 보기
- 폰 하나에 PC 여러 대(Windows, macOS, Linux): 어느 PC가 답하는지 보여 주고 골라 쓰기
- 대화 기록을 폰에 저장
- 개발자용: 내 PC의 Claude Code 세션을 폰에서(목록, 새로 시작, 열기, 멈추기, 폴더 열어 두기)
- 한국어와 영어, 밝은 화면과 어두운 화면

## 필요한 것

- Windows, macOS, Linux 중 하나를 쓰는 PC
- [Tailscale](https://tailscale.com) 계정(개인은 무료). Tailscale을 따로 설치하지 않습니다. 앱과 PC 에이전트 안에 들어 있고, 폰에서 VPN을 켤 필요도 없습니다. 양쪽에서 한 번씩 로그인하면 됩니다
- PC에 설치하고 로그인해 둔 공식 AI 프로그램 하나 이상: Claude Code, Codex, OpenCode. 둘 이상이면 종합 답변과 토론이 나오고, '내 PC'(Claude Code 세션)에는 Claude Code가 있어야 합니다. 이미 쓰는 구독과 무료 계정을 그대로 쓰며, 그 로그인 정보는 읽거나 옮기지 않습니다.
- 안드로이드 폰 (iOS는 나중에)

### Tailscale이 뭔가요?

내 기기들만 들어오는 사설망을 만들어 주는 서비스입니다(Tailscale사, 개인은 무료). Askorus 안에 들어 있습니다.

- **왜 필요한가요.** 폰이 밖에서도 집의 PC에 닿아야 합니다. 보통은 PC를 인터넷에 열거나 중간에 서버를 둬야 하는데, Askorus는 둘 다 하지 않습니다. 대신 폰과 PC가 같은 Tailscale 계정으로 로그인해 서로를 찾습니다.
- **무엇이 지켜지나요.** 내 계정에 로그인한 기기만 PC에 닿습니다. 폰과 PC 사이는 암호화되어, Tailscale의 중계 서버를 거칠 때도 Tailscale은 내용을 읽지 못합니다. 그 위에 PC에서 한 번 허용한 폰만 받습니다.
- **Tailscale이 아는 것.** 계정(메일), 기기 이름, 접속한 주소와 시간입니다. 질문과 답은 모릅니다.
- **비밀번호.** 로그인은 브라우저의 Tailscale 화면에서 합니다. Askorus는 비밀번호를 받지 않습니다.

앱에서도 로그인을 물을 때 같은 설명을 볼 수 있습니다('Tailscale이 뭔가요?').

## PC 에이전트 설치

에이전트는 Windows, macOS, Linux에서 돕니다. 어디서나 한 줄이고, 관리자 권한은 필요 없습니다.

**Windows.** PowerShell을 열고(관리자 권한이 아닌 보통 창) 아래 한 줄을 붙여 넣으세요.

```powershell
irm https://github.com/heeo-dev/askorus/releases/latest/download/install.ps1 | iex
```

**macOS와 Linux.** 터미널을 열고 아래 한 줄을 붙여 넣으세요(`sudo` 없이, 본인 계정으로).

```sh
curl -fsSL https://github.com/heeo-dev/askorus/releases/latest/download/install.sh | sh
```

스크립트가 GitHub 배포판에서 내 운영체제에 맞는 에이전트를 받아 SHA-256을 확인하고, `%LOCALAPPDATA%\Programs\Askorus`(Windows)나 `~/.local/bin`(macOS, Linux)에 둔 뒤 `askorus-agent setup`을 시작합니다. 업데이트할 때도 같은 줄을 다시 실행하면 됩니다.

setup은 차례로 안내합니다. 에이전트를 PATH에 넣고, AI 프로그램을 점검하고, 에이전트를 켜고 로그인할 때 켜지게 합니다. 그다음 이 PC를 Tailscale에 로그인할 링크를(같은 링크의 QR도) 보여 줍니다. 한 번만 하면 됩니다. 마지막으로 짝 맺기 창을 엽니다. 앱에서 같은 계정으로 Tailscale에 로그인하고 목록에 뜬 내 PC를 누르면 됩니다. QR을 찍을 필요는 없습니다(PC가 목록에 안 보이는 폰을 위해 QR도 같이 보여 줍니다). 이 창은 5분 동안 열려 있습니다. 그때까지 앱이 준비되지 않아도 괜찮습니다. 나중에 앱에서 PC를 누르고 PC 화면에 뜨는 작은 창에서 허용하거나, `askorus-agent pair`를 다시 실행하면 됩니다.

macOS와 Linux에서도 명령은 Windows와 같습니다. 운영체제마다 다른 점은 이렇습니다.

- **Linux:** 로그인할 때 켜기는 systemd 사용자 단위입니다. 아무도 로그인하지 않는 컴퓨터에서는 `loginctl enable-linger`를 해 두면 부팅할 때 켜집니다. 폰을 허용할지 묻는 창은 `zenity`나 `kdialog`와 화면이 있어야 뜹니다. 없으면 `askorus-agent pair`를 열어 두고 짝을 맺으세요.
- **macOS:** 로그인할 때 켜기는 `~/Library/LaunchAgents`의 LaunchAgent입니다. 다음 로그인부터 적용되고, 지금 바로 켜려면 `askorus-agent start`를 실행합니다.
- macOS용 에이전트는 클라우드의 Mac에서 자동 점검(Go 테스트, 설치 스크립트, launchd 아래에서 켜기·끄기·로그인할 때 켜기)으로 확인했습니다. 실제 AI 프로그램이 있는 사람의 Mac에서는 아직 써 보지 못했습니다. 안 되는 것이 있으면 [이슈로 알려 주세요](https://github.com/heeo-dev/askorus/issues).

<details>
<summary>실행하기 전에 스크립트 읽어 보기</summary>

```powershell
irm https://github.com/heeo-dev/askorus/releases/latest/download/install.ps1 -OutFile install.ps1
notepad install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

macOS와 Linux에서는:

```sh
curl -fsSL https://github.com/heeo-dev/askorus/releases/latest/download/install.sh -o install.sh
less install.sh
sh install.sh
```

원본은 [scripts/install.ps1](scripts/install.ps1)과 [scripts/install.sh](scripts/install.sh)입니다.
</details>

**초기 배포판에는 게시자 서명이 아직 없습니다.** Windows가 '알 수 없는 게시자'라고 알릴 수 있고, Windows 11에서 스마트 앱 컨트롤이 켜져 있으면 에이전트가 아예 실행되지 않을 수 있습니다. macOS용 에이전트에도 Apple 개발자 서명이 없습니다. 위의 한 줄로 설치하면 실행되지만, 브라우저로 내려받은 파일은 macOS가 막을 수 있습니다. 받은 파일이 배포판의 SHA-256과 다르면 두 스크립트 모두 멈춥니다.

### AI 에이전트에게 맡기기

PC의 Claude Code나 Codex에 내 운영체제에 맞는 글을 붙여 넣으세요. 스크립트를 먼저 읽어 보고, 내가 허락하면 읽은 그 파일로 설치하고, 빠진 것을 쉽게 설명해 줍니다.

<details>
<summary>붙여 넣을 글 (Windows)</summary>

```text
이 Windows PC에 Askorus PC 에이전트(https://github.com/heeo-dev/askorus) 설치를 도와줘.
1. 설치 스크립트를 파일로 저장해 읽고, 무엇을 하는지 몇 줄로 알려 줘:
   irm https://github.com/heeo-dev/askorus/releases/latest/download/install.ps1 -OutFile install.ps1
   ASKORUS_로 시작하는 환경 변수가 이미 있으면 먼저 알려 줘.
2. 내가 좋다고 하면, 다시 받지 말고 1에서 읽은 바로 그 파일을 관리자 권한이 아닌 보통 PowerShell에서 setup 단계 없이 실행해 줘:
   $env:ASKORUS_NO_SETUP='1'; powershell -ExecutionPolicy Bypass -File .\install.ps1
   끝나면 ASKORUS_NO_SETUP을 원래대로 되돌려 줘(없었으면 지우기).
3. 그다음 명령은 모두 설치된 파일의 전체 경로로 실행해 줘:
   & "$env:LOCALAPPDATA\Programs\Askorus\askorus-agent.exe" <명령>
   "path on", "check" 순서로 실행하고, [X]와 [!] 줄마다 무슨 뜻인지와 내가 할 일을 쉬운 말로, [i] 줄 중 내가 직접 할 일(Remote Control 동의, 폴더 신뢰)도 알려 줘.
4. check에 [X]가 없어지면 "start"와 "autostart on"을 실행하고, "check"를 한 번 더 실행해 줘. 이제 이 PC가 Tailscale에 로그인됐는지 나와.
5. 로그인되어 있지 않으면, 내 PowerShell 창에서 "askorus-agent login"을 실행하고 나오는 링크를 열라고 알려 줘. 로그인은 내가 직접 해. Tailscale을 따로 설치할 필요는 없어.
6. 마지막으로, 내 PowerShell 창에서 "askorus-agent pair"를 실행하고 Askorus 앱(같은 Tailscale 계정으로 로그인)에서 이 PC를 누르라고 알려 줘. 어느 폰이 들어왔는지와 내 폰에도 보이는 두 자리 숫자가 나오니, 내 폰이 맞는지는 내가 확인할게.
지킬 것:
- 위의 명령만 실행하고, 실행하기 전마다 나에게 물어 줘.
- 관리자 권한으로 실행하지 말고, 너(AI)의 권한 확인이나 샌드박스를 건너뛰는 옵션은 절대 쓰지 마.
- 어떤 AI 프로그램의 로그인 파일도 읽거나 옮기지 마(예: ~/.claude/.credentials.json).
- Tailscale 로그인, Claude 서비스·세션 끄기, 동의 질문은 내 몫이야. 할 일을 보여 주고 내가 하게 해 줘.
- "setup", "login", "pair"는 실행하지 마.
```

</details>

<details>
<summary>붙여 넣을 글 (macOS, Linux)</summary>

```text
이 컴퓨터(macOS 또는 Linux)에 Askorus PC 에이전트(https://github.com/heeo-dev/askorus) 설치를 도와줘.
1. 설치 스크립트를 파일로 저장해 읽고, 무엇을 하는지 몇 줄로 알려 줘:
   curl -fsSL https://github.com/heeo-dev/askorus/releases/latest/download/install.sh -o install.sh
   ASKORUS_로 시작하는 환경 변수가 이미 있으면 먼저 알려 줘.
2. 내가 좋다고 하면, 다시 받지 말고 1에서 읽은 바로 그 파일을 내 계정으로, sudo 없이, setup 단계 없이 실행해 줘:
   ASKORUS_NO_SETUP=1 sh install.sh
3. 그다음 명령은 모두 설치된 파일의 전체 경로로 실행해 줘:
   "$HOME/.local/bin/askorus-agent" <명령>
   "path on", "check" 순서로 실행하고, [X]와 [!] 줄마다 무슨 뜻인지와 내가 할 일을 쉬운 말로, [i] 줄 중 내가 직접 할 일(Remote Control 동의, 폴더 신뢰)도 알려 줘.
4. check에 [X]가 없어지면 "start"와 "autostart on"을 실행하고, "check"를 한 번 더 실행해 줘. 이제 이 PC가 Tailscale에 로그인됐는지 나와.
5. 로그인되어 있지 않으면, 내 터미널 창에서 "askorus-agent login"을 실행하고 나오는 링크를 열라고 알려 줘. 로그인은 내가 직접 해. Tailscale을 따로 설치할 필요는 없어.
6. 마지막으로, 내 터미널 창에서 "askorus-agent pair"를 실행하고 Askorus 앱(같은 Tailscale 계정으로 로그인)에서 이 PC를 누르라고 알려 줘. 어느 폰이 들어왔는지와 내 폰에도 보이는 두 자리 숫자가 나오니, 내 폰이 맞는지는 내가 확인할게.
지킬 것:
- 위의 명령만 실행하고, 실행하기 전마다 나에게 물어 줘.
- sudo나 root로 실행하지 말고, 너(AI)의 권한 확인이나 샌드박스를 건너뛰는 옵션은 절대 쓰지 마.
- 어떤 AI 프로그램의 로그인 파일도 읽거나 옮기지 마(예: ~/.claude/.credentials.json).
- Tailscale 로그인, Claude 서비스·세션 끄기, 동의 질문은 내 몫이야. 할 일을 보여 주고 내가 하게 해 줘.
- "setup", "login", "pair"는 실행하지 마.
```

</details>

## 안 될 때

먼저 PC에서 `askorus-agent check`를 실행하세요. 빠진 것을 줄마다 할 일과 함께 알려 줍니다. `[X]`는 꼭 고쳐야 하는 것, `[!]`는 주의, `[i]`는 사람이 직접 해야 하는 것입니다.

- **폰이 PC에 닿지 않는다고 합니다.** PC에서 에이전트가 돌고 있고 Tailscale에 로그인되어 있어야 합니다. `askorus-agent status`가 둘 다 보여 줍니다. 꺼져 있으면 `askorus-agent start`, 로그인이 안 되어 있으면 `askorus-agent login`을 실행하세요. 로그인할 때 켜지게 하려면 `askorus-agent autostart on`.
- **앱의 목록에 내 PC가 안 보입니다.** 앱은 로그인한 Tailscale 계정의 기기 가운데에서 PC를 찾습니다. 에이전트가 켜져 있고 같은 계정으로 로그인돼 있는지 보세요(`askorus-agent status`). 포트를 8790이 아닌 값으로 바꾼 PC는 QR로 짝을 맺습니다. `askorus-agent pair`를 실행하고 앱에서 'QR이나 링크로 짝 맺기'를 쓰세요. Tailscale 망이 새 기기의 승인을 요구하면 Tailscale 관리 화면에서 폰과 PC를 승인하세요. 접근 규칙을 직접 써 둔 망이라면 내 기기끼리 8790 포트로 닿을 수 있어야 합니다.
- **PC 앞에 없는데 폰을 추가하고 싶습니다.** 이미 짝 맺은 폰에서 허용할 수 있습니다. 그 폰에서 Askorus를 열면 새 폰의 이름과 두 자리 숫자가 함께 요청이 뜹니다.
- **폰이 Tailscale에 로그인되어 있지 않다고 하거나, 이 PC가 내 Tailscale 계정에 없다고 합니다.** 앱의 설정 > Tailscale에서 PC의 에이전트와 같은 계정으로 로그인하세요(어느 계정인지는 PC에서 `askorus-agent status`). 계정을 바꾸려면 앱에서는 '로그아웃 / 계정 바꾸기'(설정 > Tailscale, 짝 맺기 화면, 'PC가 내 계정에 없습니다' 상자), PC에서는 `askorus-agent logout` 뒤 `askorus-agent login`입니다. 바꾼 뒤에는 다시 짝 맺습니다. 이미 Tailscale을 쓰고 있다면 Askorus에는 그 계정을 써도 되고 따로 만든 계정을 써도 됩니다. 앱과 에이전트만 같은 계정이면 됩니다. 브라우저가 이미 Tailscale에 로그인돼 있으면 그 계정으로 붙이겠냐고 묻습니다. 다른 계정을 쓰려면 그 화면에서 계정을 바꾸거나 시크릿 창에서 링크를 여세요.
- **폰에 '지금 PC에서 허용해 줄 수 있는 곳이 없습니다'라고 나오거나, PC에 창이 뜨지 않습니다.** 작은 창은 화면이 있어야 뜹니다. Linux에서는 `zenity`나 `kdialog`도 있어야 하고, 서버나 SSH 접속에는 뜨지 않습니다. PC에서 `askorus-agent pair`를 실행한 뒤 앱에서 PC를 다시 누르세요. 실행해 둔 동안에는 묻지 않고 바로 받습니다. 이미 짝 맺은 폰에서 허용해도 됩니다.
- **모르는 폰이 짝 맺어졌거나, 터미널의 숫자가 내 폰의 숫자와 다릅니다.** `askorus-agent devices`가 짝 맺은 폰을 보여 주고, `askorus-agent devices revoke <id나 이름>`으로 끊습니다.
- **몇 달 잘 되다가 갑자기 안 됩니다.** Tailscale 로그인은 기본으로 약 180일 뒤 끝납니다. `askorus-agent check`가 2주 전부터 알리고, 앱의 설정 > Tailscale에 끝나는 날짜가 보입니다. 다시 로그인하거나(PC는 `askorus-agent login`, 앱은 설정 > Tailscale), Tailscale 관리 화면에서 기기의 키 만료를 끄세요. 짝 맺기는 그대로 남습니다.
- **새 Claude Code 세션이 시작되지 않습니다.** Claude Code는 폴더마다 한 번 신뢰할지 묻습니다. PC에서 그 폴더로 가서 `claude`를 한 번 실행하고 허용하세요.
- **폴더 열어 두기가 '켜는 중'에 머물거나 실패합니다.** Claude Code에서 Remote Control을 한 번 켜야 합니다. PC에서 `claude remote-control`을 실행해 `y`로 답하고 Ctrl+C로 끄세요.
- **AI가 목록에 없습니다.** PC에 설치하고 로그인한 뒤 `check`를 다시 실행하세요. `autostart on` 뒤에 설치한 프로그램은 흔한 설치 폴더에서 찾습니다. 못 찾으면 `autostart on`을 다시 실행하세요.

그래도 안 되면 `askorus-agent check` 결과를 붙여 [이슈를 열어 주세요](https://github.com/heeo-dev/askorus/issues/new/choose)(계정, PC 이름, Tailscale 주소는 지우고).

## 0.2.x에서 올리기

0.3.0은 폰이 PC에 닿는 방식이 바뀌었습니다. Tailscale이 앱과 에이전트 안에 들어갔고, 짝 맺기는 앱에서 PC를 눌러서 합니다. 앱과 에이전트를 함께 올리고 짝을 다시 맺으세요.

1. PC에서 설치 한 줄을 다시 실행합니다. 그다음 `askorus-agent login`을 한 번 실행하고 나오는 링크를 엽니다. 이 PC가 Tailscale에 로그인됩니다.
2. 폰에 0.3.0 앱을 설치하고, 앱에서 같은 계정으로 Tailscale에 로그인한 뒤 'PC와 짝 맺기'에서 내 PC를 누릅니다. 설정에서 옛 PC 항목은 끊습니다.
3. Askorus는 이제 폰의 Tailscale 앱과 PC의 Tailscale을 쓰지 않습니다. 다른 데 쓰고 있다면 그대로 두세요. 에이전트용으로 걸어 둔 옛 `tailscale serve` 설정은 저절로 지워지지 않습니다. PC에서 `tailscale serve status`로 볼 수 있고, `tailscale serve --https=<포트> off`로 끕니다.

## 지우기

```
askorus-agent uninstall
```

무엇을 되돌릴지 보여 주고 먼저 묻습니다. 이 PC를 Tailscale에서 로그아웃하고, 에이전트를 멈추고, 로그인할 때 켜기와 PATH 등록을 없애고, 짝 맺은 폰을 모두 끊습니다. `-all`을 붙이면 에이전트의 설정과 로그도 지웁니다. 그다음 프로그램 파일은 직접 지우세요(Windows는 `%LOCALAPPDATA%\Programs\Askorus`, macOS와 Linux는 `~/.local/bin/askorus-agent`). 폰에서는 설정에서 PC를 끊고 설정 > Tailscale에서 로그아웃하거나, 앱을 지우면 됩니다. 그 뒤에도 Tailscale 관리 화면에 'askorus-'로 시작하는 기기가 남아 있으면(로그아웃하지 않고 앱을 지웠거나 에이전트가 꺼져 있었을 때) 거기서 지우세요.

## 안드로이드 앱 설치

앱은 아직 Google Play에 없습니다. 그때까지는 배포판마다 미리보기 APK를 같이 올립니다.

1. 폰에서 [최신 배포판](https://github.com/heeo-dev/askorus/releases/latest)을 열고 `askorus-android-<판>.apk`를 받습니다.
2. 받은 파일을 엽니다. 안드로이드가 브라우저에서 앱을 설치해도 되는지 물으면 이번 설치에 허용합니다. Play 프로텍트가 앱을 먼저 검사하겠다고 물을 수 있습니다. 스토어 밖에서 받은 앱이면 늘 묻는 것입니다.
3. Askorus를 열고 PC와 짝을 맺습니다. 앱이 안내하면 Tailscale에 로그인하고(PC와 같은 계정), 목록에 뜬 내 PC를 누르고 '짝 맺기'를 누릅니다. 폰에 두 자리 숫자가 보입니다. PC 화면에 작은 창이 떠서 이 폰을 허용할지 한 번 묻고 같은 숫자를 보여 줍니다. 숫자가 같을 때만 허용하세요. 그다음 폰에는 '짝을 맺었습니다'와 숫자가 '확인'을 누를 때까지 보입니다. (터미널에서 `askorus-agent pair`나 `setup`이 기다리는 동안에는 묻지 않고 바로 받습니다.)

<p align="center">
  <img src="docs/screenshots/ko/6-pair-list.png" width="200" alt="PC와 짝 맺기: Tailscale에 로그인하면 내 계정에서 찾은 PC가 목록에 뜬다">
  <img src="docs/screenshots/ko/7-pair-number.png" width="200" alt="PC의 허용을 기다리는 화면: 폰에 두 자리 숫자가 보이고 PC에도 같은 숫자가 보인다">
</p>

미리보기 APK에서 알아 둘 것:

- **자동 업데이트가 없습니다.** 새 APK를 받아 그 위에 설치하면 업데이트됩니다. 기록과 짝 맺기는 그대로 남습니다. (0.2.x에서 올릴 때는 예외입니다. [0.2.x에서 올리기](#02x에서-올리기)를 보세요.)
- **나중에 Google Play 앱으로 옮길 때는 다시 설치해야 합니다.** Google Play는 앱을 다른 키로 서명해서, 이 앱 위에 Play 앱을 덮어 설치할 수 없습니다. 이 앱을 먼저 지워야 하고(앱에 둔 기록도 같이 지워집니다) 짝 맺기를 다시 합니다.
- **파일을 직접 확인할 수 있습니다.** 배포 글에 APK의 SHA-256이 있습니다.

앱을 만드는 데 쓰인 오픈소스는 설정 > 정보 > 오픈소스 라이선스에서 볼 수 있습니다.

## 소스에서 빌드

PC 에이전트는 Go 프로그램 하나입니다(Go 1.27).

```powershell
cd agent
go build -trimpath -tags ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail -o askorus-agent.exe ./cmd/askorus-agent
.\askorus-agent.exe setup         # PATH, 점검, 켜기, 로그인할 때 켜기, Tailscale 로그인, 짝 맺기를 차례로
```

macOS와 Linux에서는:

```sh
cd agent
go build -trimpath -tags ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail -o askorus-agent ./cmd/askorus-agent
./askorus-agent setup
```

태그는 에이전트가 넣지 말아야 할 Tailscale 라이브러리의 부분(PC 주소에 여는 포트, 진단, 로그 올리기)을 뺍니다. 태그 없이 빌드한 것은 Tailscale 기기를 켜지 않습니다.

하나씩 하려면: `path on`(PATH에 넣기), `check`(빠진 것과 고치는 방법), `start`(백그라운드로 켜기, 일반 권한), `autostart on`(로그인할 때 켜기), `login`(이 PC를 링크로 Tailscale에 로그인), `pair`(짝 맺기 창 열기: 앱에서 이 PC를 누른 폰을 묻지 않고 받습니다. PC가 목록에 안 보이는 폰을 위해 QR도 보여 줍니다). 세 운영체제의 배포 파일은 `scripts/release.ps1`이 만듭니다.

앱은 React Native와 Expo로 만들었습니다(`app/`). 앱의 Tailscale 부분은 Go 라이브러리(`tsbridge/`)이고 안드로이드 앱 안에 함께 빌드됩니다. 안드로이드 앱 빌드는 [docs/android-release.md](docs/android-release.md)를 보세요.

## 개인정보

- 우리 서버, 분석, 광고, 오류 보고 도구가 없습니다.
- 질문은 앱과 에이전트에 들어 있는 Tailscale을 거쳐 폰에서 내 PC로 갑니다. 두 기기 사이에서 암호화되고, 내 Tailscale 계정에 로그인한 기기끼리만 통합니다. PC의 공식 AI 프로그램이 각 AI 회사로 보냅니다.
- 앱과 에이전트는 로그인하고 서로를 찾기 위해 Tailscale의 서버와 통신합니다. 두 기기가 직접 연결되지 못할 때는 Tailscale의 중계 서버가 암호화된 내용을 그대로 전달합니다. Tailscale로 진단 로그를 보내지 않습니다.
- 사회자 AI의 회사는 질문과 모든 답을 받고, 토론하면 답한 AI들의 회사가 서로의 답을 받습니다.
- 앱의 대화 기록은 폰에 저장됩니다. PC의 AI 프로그램에도 자체 기록이 남을 수 있고, 에이전트는 답을 PC 메모리에 하루 둡니다.

자세히: [docs/privacy.md](docs/privacy.md)

## 저장소

| 폴더 | 내용 |
|---|---|
| `agent/` | PC 에이전트(Go): 자기 Tailscale 기기에서 여는 API, AI 프로그램마다의 어댑터 |
| `app/` | 폰 앱(React Native, Expo) |
| `tsbridge/` | 앱의 Tailscale 부분(Go, 안드로이드 앱 안에 빌드됨) |
| `docs/` | 계획, 테스트, 개인정보, 스토어 설명, 브랜드(`docs/brand`) |
| `scripts/` | 테스트와 도구(main에 합치기 전 기준은 `scripts/test-all.ps1`, macOS와 Linux에서는 [CONTRIBUTING.md](CONTRIBUTING.md) 참고) |

## 공식 제품이 아닙니다

Askorus는 독립 프로젝트입니다. 어떤 AI 회사의 공식 제품도 아니며, Anthropic, OpenAI, Google 등과 제휴하거나 보증을 받지 않았습니다. Claude, Codex 등의 이름은 각 회사의 상표입니다.

## 기여

버그 신고와 제안은 [이슈](https://github.com/heeo-dev/askorus/issues/new/choose)로 받습니다. 빌드, 테스트, 변경을 보내는 방법은 [CONTRIBUTING.md](CONTRIBUTING.md)에 있습니다.

## 보안

보안 문제를 찾으셨다면 비공개로 알려 주세요. [SECURITY.md](SECURITY.md)에 방법이 있습니다.

## 라이선스

Apache-2.0. [LICENSE](LICENSE)를 보세요. Askorus에 들어 있거나 Askorus를 만드는 데 쓰인 다른 사람의 저작물은 [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md)에, 내장 Tailscale의 Go 모듈은 [THIRD-PARTY-GO.md](THIRD-PARTY-GO.md)에 적었습니다.

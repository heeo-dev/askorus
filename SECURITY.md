# Security

한국어는 [아래](#보안-한국어)에 있습니다.

## Reporting a vulnerability

Please report security problems in private, not in a public issue.

- Use **Report a vulnerability** on the repository's
  [Security tab](https://github.com/heeo-dev/askorus/security/advisories/new)
  (GitHub private vulnerability reporting). Only the maintainer sees the report.
- Say what you found, how to reproduce it, and which version or commit you
  used. A short example helps more than a long description.
- Do not test against a PC, a phone or an account that is not yours.

You can expect a first reply within a week. Askorus is maintained by one
person in spare time, so a fix may take longer than that; you will be told
what is planned. Once a fix is released the problem is described in the
release notes, with credit to you if you want it.

## What counts

Askorus has two parts: the PC agent (`agent/`, a program that runs on your
own PC and starts your AI programs) and the phone app (`app/`). There is no
Askorus server. Reports of these kinds are especially useful:

- reaching the PC agent without a paired device's token, or after the device
  was unpaired
- making the agent run a command, or an AI tool, that it is not meant to run
- reading or changing files on the PC through a question, an answer or a name
- a web page, a link or a QR code that makes the app or the agent act for
  someone else
- an install script (`scripts/install.ps1`, `scripts/install.sh`) installing
  something other than the released file
- getting a phone paired without the person allowing it (the window on the
  PC, an open pairing window, or a paired phone)
- reaching the agent from a device that is not in the PC owner's Tailscale
  account, or the agent or the app sending anything outside the Tailscale
  tunnel, other than the agent's update check (from 0.5.0 it asks this
  repository's GitHub releases for the latest version when a phone asks)
- making the agent install a program that is not a release signed with the
  project's keys, or a version older than the one it runs
- a phone kept from updating (`askorus-agent devices update <id> off`) still
  starting an update, or the agent asking GitHub while updates are off
  (`askorus-agent update off`)

Not in scope: what an AI company's own program or service does with your
question, problems that need administrator (root) rights or the same user account
on the PC, whichever system it runs, and that the releases do not carry a Windows or Apple
publisher signature yet (planned, see the README).

## Supported versions

Only the latest release gets fixes.

---

## 보안 (한국어)

### 취약점 알리기

보안 문제는 공개 이슈가 아니라 비공개로 알려 주세요.

- 저장소 [Security 탭](https://github.com/heeo-dev/askorus/security/advisories/new)의
  **Report a vulnerability**를 씁니다(GitHub 비공개 취약점 제보). 메인테이너만 봅니다.
- 무엇을 찾았는지, 어떻게 재현하는지, 어느 판이나 커밋에서 봤는지 적어 주세요.
  긴 설명보다 짧은 예 하나가 도움이 됩니다.
- 자기 것이 아닌 PC, 폰, 계정에는 시험하지 마세요.

첫 답은 1주 안에 드립니다. Askorus는 한 사람이 남는 시간에 돌보는 프로젝트라
고치는 데는 더 걸릴 수 있고, 어떻게 할지 알려 드립니다. 고친 판을 낸 뒤 배포
글에 문제를 적고, 원하시면 알려 주신 분을 함께 적습니다.

### 받는 범위

Askorus는 PC 에이전트(`agent/`, 내 PC에서 돌며 내 AI 프로그램을 실행)와 폰
앱(`app/`) 두 부분이고, Askorus 서버는 없습니다. 이런 제보가 특히 도움이 됩니다.

- 짝 맺은 기기의 토큰 없이, 또는 기기를 끊은 뒤에 PC 에이전트에 닿는 것
- 에이전트가 정해지지 않은 명령이나 AI 도구를 실행하게 하는 것
- 질문, 답, 이름을 통해 PC의 파일을 읽거나 바꾸는 것
- 웹페이지, 링크, QR이 앱이나 에이전트를 남 대신 움직이게 하는 것
- 설치 스크립트(`scripts/install.ps1`, `scripts/install.sh`)가 배포된 파일이 아닌 것을 설치하는 것
- 사람이 허용하지 않았는데 폰이 짝 맺어지는 것(PC 화면의 창, 열어 둔 짝 맺기 창, 짝 맺은 폰)
- PC 주인의 Tailscale 계정에 없는 기기가 에이전트에 닿는 것, 에이전트나 앱이 Tailscale 터널 밖으로 무언가를 보내는 것(에이전트의 업데이트 확인은 빼고: 0.5.0부터 폰이 물을 때 이 저장소의 GitHub 배포판에 최신 판을 묻습니다)
- 프로젝트의 키로 서명된 배포판이 아닌 것이나 지금보다 옛 판을 에이전트가 설치하게 하는 것
- 업데이트를 막은 폰(`askorus-agent devices update <id> off`)이 업데이트를 시키는 것, 업데이트를 껐는데(`askorus-agent update off`) 에이전트가 GitHub에 묻는 것

범위 밖: AI 회사의 프로그램과 서비스가 질문을 다루는 방식, 어느 운영체제든 PC의 관리자(root)
권한이나 같은 사용자 계정이 있어야 되는 문제, 배포판에 Windows나 Apple의 게시자 서명이
아직 없는 것(예정된 일, README 참고).

### 지원하는 판

가장 최근 배포판만 고칩니다.

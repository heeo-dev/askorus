# Asksemble 개인정보처리방침 (초안)

시행일: (Play에 올릴 때 적는다)

Asksemble은 사용자의 폰에서 사용자의 PC에 있는 코딩 AI 세션을 보고 켜는 앱이다. 개발자는 서버를 두지 않고, 사용자의 데이터를 받지 않는다.

- **모으는 정보**: 없다. 분석·광고·오류 보고 도구를 넣지 않는다.
- **폰에 저장하는 정보**: 짝 맺은 PC의 이름과 주소, PC가 준 기기 토큰, 새 세션 기본값. 폰의 보안 저장소에만 두고, 앱을 지우거나 설정에서 PC를 끊으면 지워진다.
- **PC에 보내는 정보**: 짝 맺을 때 사용자가 정한 폰 이름, 그리고 사용자가 누른 동작(목록 보기, 열기, 새 세션, 멈추기, 지우기, 폴더 열어 두기). 통신은 사용자의 Tailscale 망 안에서 사용자의 PC와만 한다.
- **카메라**: PC 화면의 짝 맺기 QR을 읽을 때만 쓴다. 사진을 저장하거나 보내지 않는다.
- **다른 앱으로 넘기기**: 세션을 열면 Claude 앱이나 브라우저로 넘긴다. 그다음은 그 서비스의 방침을 따른다. Asksemble은 어떤 AI의 로그인 정보도 읽거나 옮기지 않는다.
- **한 번에 묻기**(Phase 2에서 더할 기능): 질문은 사용자의 PC에서 사용자가 로그인해 둔 AI 프로그램(Claude Code, Codex, OpenCode)이 각 회사로 보낸다. AI가 웹을 검색하면 검색어가 그 회사의 검색(Anthropic, OpenAI)이나 OpenCode가 고른 검색 제공자(예: Firecrawl)로 간다. 웹 페이지 읽기는 사용자의 PC에서 그 주소로 직접 접속한다. 개발자의 서버는 거치지 않는다.
- **어린이**: 어린이를 대상으로 하지 않는다.
- **문의**: (공개 전에 연락처를 적는다)

Asksemble은 Anthropic의 공식 제품이 아니다.

---

# Asksemble Privacy Policy (draft)

Effective date: (set when published on Google Play)

Asksemble lets you see and start the coding AI sessions on your own PC from your phone. The developer runs no server and receives none of your data.

- **Data we collect**: none. The app has no analytics, ads, or crash reporting.
- **Data kept on your phone**: the name and address of each paired PC, the device token the PC issued, and your new-session defaults. They stay in the phone's secure storage and are removed when you uninstall the app or disconnect the PC in Settings.
- **Data sent to your PC**: the phone name you choose when pairing, and the actions you take (list, open, new session, stop, remove, keep folder open). Traffic goes only to your own PC inside your Tailscale network.
- **Camera**: used only to scan the pairing QR code on your PC screen. No pictures are saved or sent.
- **Handing off to other apps**: opening a session hands you off to the Claude app or your browser, whose own policies then apply. Asksemble never reads or moves the sign-in data of any AI.
- **Ask all at once** (a Phase 2 feature): your question is sent from your PC to each AI company by the AI programs you signed in to there (Claude Code, Codex, OpenCode). When an AI searches the web, the search terms go to that company's search (Anthropic, OpenAI) or to a search provider OpenCode picks (for example Firecrawl). Reading a web page connects from your PC straight to that address. Nothing passes through a developer server.
- **Children**: the app is not directed at children.
- **Contact**: (add before publishing)

Asksemble is not an official Anthropic product.

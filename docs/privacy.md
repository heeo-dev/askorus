# Asksemble 개인정보처리방침 (초안)

시행일: (Play에 올릴 때 적는다)

Asksemble은 여러 AI에게 한 번에 묻고 답이 같은 곳과 다른 곳을 보여 주는 앱이고, 폰에서 사용자의 PC에 있는 코딩 AI 세션을 보고 켜는 앱이다. AI에게 묻고 비교하는 일은 사용자의 PC가 한다. 개발자는 서버를 두지 않고, 사용자의 데이터를 받지 않는다.

- **모으는 정보**: 없다. 분석·광고·오류 보고 도구를 넣지 않는다.
- **폰에 저장하는 정보**: 짝 맺은 PC의 이름과 주소, PC가 준 기기 토큰, 새 세션 기본값, 묻기 기본값(단계, 고른 PC, 끈 AI, AI별 모델), 비교와 종합 답변 설정, 화면 테마. 기기 토큰은 폰의 보안 저장소에 둔다. 앱을 지우면 모두 지워지고, 설정에서 PC를 끊으면 그 PC의 정보가 지워진다. 대화 기록(질문, 각 AI의 답, 비교 결과, 종합 답변)은 앱 안의 저장소에 최근 100개(합쳐 약 20MB)까지 남고, 넘으면 오래된 것부터 지워진다. 서랍에서 대화를 길게 눌러 하나를, 설정의 '기록 모두 지우기'로 전부를 지울 수 있다(폰에서만 지우고 PC의 각 AI 기록은 남는다). 앱은 Android 백업에서 빠져 있어 기록이 Google 백업으로 나가지 않는다.
- **PC에 보내는 정보**: 짝 맺을 때 사용자가 정한 폰 이름, 사용자가 누른 동작(목록 보기, 열기, 새 세션, 멈추기, 지우기, 폴더 열어 두기), 사용자가 쓴 질문과 비교·종합 답변 요청. 통신은 사용자의 Tailscale 망 안에서 사용자의 PC와만 한다.
- **카메라**: PC 화면의 짝 맺기 QR을 읽을 때만 쓴다. 사진을 저장하거나 보내지 않는다.
- **다른 앱으로 넘기기**: 세션을 열면 Claude 앱이나 브라우저로 넘긴다. 그다음은 그 서비스의 방침을 따른다. Asksemble은 어떤 AI의 로그인 정보도 읽거나 옮기지 않는다.
- **한 번에 묻기**: 질문은 사용자의 PC에서 사용자가 로그인해 둔 AI 프로그램(Claude Code, Codex, OpenCode)이 각 회사로 보낸다. AI가 웹을 검색하면 검색어가 그 회사의 검색(Anthropic, OpenAI)이나 OpenCode가 고른 검색 제공자(예: Firecrawl, Exa)로 간다. 웹 페이지 읽기는 사용자의 PC에서 그 주소로 직접 접속한다. 개발자의 서버는 거치지 않는다. 질문과 답은 각 AI 프로그램의 대화 기록으로 PC에 남고(후속 질문에 쓴다), 사용자가 그 프로그램에 설정해 둔 확장(훅·플러그인)에도 전해질 수 있다.
- **비교(판정)**: 답을 비교할 때 PC는 판정을 맡은 AI 프로그램에 질문과 다른 AI들의 답을 보내고(이어서 물었으면 그 AI가 이미 받은 앞 질문 3개까지도), 그 AI의 회사가 이것을 받는다. 한 AI가 답하지 못해 답이 둘뿐이면, 그 질문에 켜 둔 다른 AI가 판정만 맡아 두 답을 받는다. 판정에는 웹 검색을 끄고 파일 쓰기와 명령을 막는다(Codex는 도구를 완전히 끌 수 없어 읽기 전용으로 두고, 도구를 쓰려 한 판정은 버린다). Claude Code와 Codex는 판정 대화를 기록으로 남기지 않지만 Codex는 자체 로그 파일을 남길 수 있다. OpenCode는 판정 뒤 대화를 지우지만 지운 글이 데이터 파일 안에 한동안 남을 수 있다. Claude Code와 Codex는 판정할 때 사용자가 설정한 훅·플러그인을 끄고, OpenCode에 설정한 확장에는 전해질 수 있다. 비교 결과는 PC 메모리에 30분, 폰에는 대화 기록 안에 남는다. 개발자의 서버는 거치지 않는다.
- **종합 답변**: 비교가 끝나면 그 질문을 받은 AI 중 하나(기본은 Claude, 설정에서 바꾸거나 끔)가 여러 답을 하나로 정리한다. 이때 PC는 그 AI 프로그램에 질문, AI 이름이 붙은 모든 답, 비교 결과를 보내고, 그 AI의 회사가 이것을 받는다. 판정과 같이 웹 검색을 끄고 파일 쓰기와 명령을 막으며, 대화가 남는 방식도 판정과 같다. 개발자의 서버는 거치지 않는다.
- **질문 정제**: 정제를 켜면 답을 받기 전에 그 질문을 받는 AI 중 하나(사회자, 설정에서 고름)가 질문과 앞 질문 3개까지를 읽고 질문의 종류·의도·깊이·언어를 정한다. 그 AI의 회사가 질문을 한 번 더 받는다. 판정·종합과 같이 웹 검색을 끄고 파일 쓰기와 명령을 막으며, 대화가 남는 방식도 같다. 정한 종류에 맞는 답 형식이 질문 뒤에 붙어 모든 AI에게 간다. 개발자의 서버는 거치지 않는다.
- **어린이**: 어린이를 대상으로 하지 않는다.
- **문의**: (공개 전에 연락처를 적는다)

Asksemble은 Anthropic, OpenAI 같은 AI 회사의 공식 제품이 아니다.

---

# Asksemble Privacy Policy (draft)

Effective date: (set when published on Google Play)

Asksemble asks several AIs at once and shows where their answers agree and where they don't, and it lets you see and start the coding AI sessions on your own PC from your phone. Your own PC does the asking and the comparing. The developer runs no server and receives none of your data.

- **Data we collect**: none. The app has no analytics, ads, or crash reporting.
- **Data kept on your phone**: the name and address of each paired PC, the device token the PC issued, your new-session defaults, your asking defaults (tier, chosen PC, AIs turned off, per-AI models), your comparison and combined-answer settings, and the theme. The device token stays in the phone's secure storage. Uninstalling the app removes all of it, and disconnecting a PC in Settings removes that PC's data. Your conversation history (questions, each AI's answers, comparison results and combined answers) is kept in the app's own storage, up to the latest 100 conversations (about 20 MB in all); older ones are deleted. You can delete one conversation by long-pressing it in the menu, or all of them with Delete all history in Settings (this deletes them from the phone only; each AI's own record on your PC stays). The app is excluded from Android backup, so the history does not go to Google backups.
- **Data sent to your PC**: the phone name you choose when pairing, the actions you take (list, open, new session, stop, remove, keep folder open), and the questions you write and the comparisons and combined answers you ask for. Traffic goes only to your own PC inside your Tailscale network.
- **Camera**: used only to scan the pairing QR code on your PC screen. No pictures are saved or sent.
- **Handing off to other apps**: opening a session hands you off to the Claude app or your browser, whose own policies then apply. Asksemble never reads or moves the sign-in data of any AI.
- **Ask all at once**: your question is sent from your PC to each AI company by the AI programs you signed in to there (Claude Code, Codex, OpenCode). When an AI searches the web, the search terms go to that company's search (Anthropic, OpenAI) or to a search provider OpenCode picks (for example Firecrawl or Exa). Reading a web page connects from your PC straight to that address. Nothing passes through a developer server. Questions and answers stay on your PC in each AI program's conversation history (follow-up questions use it), and may also reach extensions (hooks, plugins) you set up in those programs.
- **Comparing (judging)**: to compare answers, your PC sends the judging AI program the question and the other AIs' answers (and, after follow-ups, up to three earlier questions that AI already received), and that AI's company receives them. If one AI could not answer and only two answers are left, another AI you turned on for that question judges them and receives both answers. Judging runs with web search off and with file writes and commands blocked (Codex cannot turn its tools off completely, so it runs read-only and a judgment that reaches for a tool is thrown away). Claude Code and Codex keep no history of a judging conversation, though Codex may keep its own log file. OpenCode deletes the judging conversation afterwards, but the deleted text may stay inside its data file for a while. Claude Code and Codex judge with your hooks and plugins turned off; extensions you set up in OpenCode may still receive it. Comparison results stay in the PC's memory for 30 minutes and in the conversation history on your phone. Nothing passes through a developer server.
- **Combined answer**: after a comparison, one of the AIs that got the question (Claude by default; change it or turn it off in Settings) writes the answers up as one. Your PC sends that AI program the question, every answer with the AIs' names, and the comparison's results, and that AI's company receives them. As with judging, web search is off and file writes and commands are blocked, and what stays behind is the same as for judging. Nothing passes through a developer server.
- **Question refining**: when refining is on, one of the AIs the question goes to (the moderator, chosen in Settings) first reads the question, with up to three earlier questions, and sets its kind, intent, depth and language; that AI's company receives the question once more. As for judging and combined answers, web search is off, file writes and commands are blocked, and the conversation is kept the same way. The answer format of that kind is added after the question for every AI. Nothing passes through a developer server.
- **Children**: the app is not directed at children.
- **Contact**: (add before publishing)

Asksemble is not an official product of any AI company, such as Anthropic or OpenAI.

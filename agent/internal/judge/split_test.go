package judge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// splitCheck checks what every Split result must hold: ids and n in order,
// Judged for the first maxUnits, offsets in the text, units in order and
// apart, and Text equal to Strip of the raw span the phone would cut.
func splitCheck(t testing.TB, id, text string, units []Unit, maxUnits int) {
	t.Helper()
	if maxUnits <= 0 {
		maxUnits = MaxUnits
	}
	// byteAt[u] is ByteOffset(text, u), made once: the phone's offsets
	at := make([]int, 0, len(text)+1)
	for b := 0; b < len(text); {
		_, size, n := mathRune(text, b)
		for range n {
			at = append(at, b)
		}
		b += size
	}
	at = append(at, len(text))
	total := len(at) - 1
	last := 0
	for i, u := range units {
		if u.N != i+1 || u.ID != fmt.Sprintf("%s:%d", id, i+1) || u.Judged != (u.N <= maxUnits) {
			t.Fatalf("unit %d: id %q n %d judged %v", i, u.ID, u.N, u.Judged)
		}
		if u.Start < last || u.End <= u.Start || u.End > total {
			t.Fatalf("unit %s: span %d..%d after %d, text of %d", u.ID, u.Start, u.End, last, total)
		}
		raw := text[at[u.Start]:at[u.End]]
		if (i < 3 || i == len(units)-1) && SliceUTF16(text, u.Start, u.End) != raw {
			t.Fatalf("unit %s: SliceUTF16 = %q, want %q", u.ID, SliceUTF16(text, u.Start, u.End), raw)
		}
		if got := Strip(u.Kind, raw); got != u.Text {
			t.Fatalf("unit %s: Strip(%q) = %q, Text %q", u.ID, raw, got, u.Text)
		}
		if u.Kind == UnitRow { // the cells' texts joined as before Unit.Math
			var cells []string
			for _, cell := range splitCells(raw) {
				cells = append(cells, Strip(UnitSentence, cell))
			}
			if want := strings.Join(strings.Fields(strings.Join(cells, " | ")), " "); u.Text != want {
				t.Fatalf("unit %s: row %q, cells joined %q", u.ID, u.Text, want)
			}
		}
		splitCheckMath(t, u, raw)
		if u.Text == "" || u.Text != strings.TrimSpace(u.Text) || !splitHasLetter(u.Text) {
			t.Fatalf("unit %s: text %q", u.ID, u.Text)
		}
		// A context goes to the phone and into every prompt line of its
		// unit, so it is bounded: a header 300 characters, a parent 120.
		if n := utf8.RuneCountInString(u.Context); n > splitHeaderChars {
			t.Fatalf("unit %s: context of %d characters", u.ID, n)
		}
		switch u.Kind {
		case UnitSentence, UnitItem, UnitRow, UnitQuote:
		default:
			t.Fatalf("unit %s: kind %q", u.ID, u.Kind)
		}
		last = u.End
	}
}

// splitCheckMath checks a unit's Math: spans in order and apart, inside
// Text on character boundaries, each the math as written: an opening mark
// and its closing one, Display for "$$" and "\[", and its characters but
// white space (and a quote's ">" marks) in the raw span in that order.
func splitCheckMath(t testing.TB, u Unit, raw string) {
	t.Helper()
	squeeze := func(s string) string {
		var b strings.Builder
		for _, r := range s { // an invalid byte is U+FFFD, as in Strip
			if !unicode.IsSpace(r) && r != '>' {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	if u.Kind == UnitRow { // a cell's "\|" is "|" in its code and math
		raw = strings.ReplaceAll(raw, `\|`, "|")
	}
	total, last := UTF16Offset(u.Text, len(u.Text)), 0
	for _, m := range u.Math {
		if m.Start < last || m.End <= m.Start || m.End > total ||
			UTF16Offset(u.Text, ByteOffset(u.Text, m.Start)) != m.Start || UTF16Offset(u.Text, ByteOffset(u.Text, m.End)) != m.End {
			t.Fatalf("unit %s: math %+v after %d in a text of %d: %q", u.ID, m, last, total, u.Text)
		}
		s := SliceUTF16(u.Text, m.Start, m.End)
		open, close := "$", "$"
		switch {
		case strings.HasPrefix(s, "$$"):
			open, close = "$$", "$$"
		case strings.HasPrefix(s, `\(`):
			open, close = `\(`, `\)`
		case strings.HasPrefix(s, `\[`):
			open, close = `\[`, `\]`
		}
		display := open == "$$" || open == `\[`
		if len(s) <= len(open)+len(close) || !strings.HasPrefix(s, open) || !strings.HasSuffix(s, close) || m.Display != display {
			t.Fatalf("unit %s: math %q display %v in %q", u.ID, s, m.Display, u.Text)
		}
		if !strings.Contains(squeeze(raw), squeeze(s)) {
			t.Fatalf("unit %s: math %q not in %q", u.ID, s, raw)
		}
		last = m.End
	}
}

// splitNorm is what the P0-6 merge check compared (judge-merge.py): no
// white space, no "|", no emphasis marks or backticks.
func splitNorm(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("|*_`~", r) {
			return -1
		}
		return r
	}, s)
}

// splitLinkRe is a Markdown link, with brackets around it; claims were
// copied without inline source links.
var splitLinkRe = regexp.MustCompile(`\(?!?\[[^\]]*\]\([^)]*\)\)?`)

// splitNumberRe is a list number that starts a text ("1. ", "2) "): an
// ordered item's first unit keeps it (2026-11-01, rsplit-15), and the
// P0-6 claims were copied without list markers.
var splitNumberRe = regexp.MustCompile(`^[0-9]{1,3}[.)][ \t]+`)

// splitDropNumber is s without one list number at its start.
func splitDropNumber(s string) string {
	return splitNumberRe.ReplaceAllString(s, "")
}

// splitFitClaims places every P0-6 claim among the units of its answer:
// exactly one unit, a run of consecutive units, or inside one unit. A unit
// is compared by its text and by its raw span without links, both without
// the list number an ordered item's first unit starts with.
func splitFitClaims(t testing.TB) (exact, run, inside int, misses []string) {
	t.Helper()
	type unitForms struct{ text, raw []string }
	forms := map[string]unitForms{}
	for _, a := range p06Answers(t) {
		var f unitForms
		units, _ := Split(a.AI, a.Answer, 0)
		for _, u := range units {
			f.text = append(f.text, splitNorm(splitDropNumber(u.Text)))
			f.raw = append(f.raw, splitNorm(splitLinkRe.ReplaceAllString(splitDropNumber(SliceUTF16(a.Answer, u.Start, u.End)), "")))
		}
		forms[fmt.Sprintf("%d-%s", a.Q, a.AI)] = f
	}
	for _, c := range p06Claims(t) {
		f := forms[fmt.Sprintf("%d-%s", c.Q, c.AI)]
		want := splitNorm(c.Text)
		name := fmt.Sprintf("%d-%s%d", c.Q, c.AI, c.N)
		switch {
		case splitFitOne(want, f.text) || splitFitOne(want, f.raw):
			exact++
		case splitFitRun(want, f.text) || splitFitRun(want, f.raw):
			run++
		case splitFitInside(want, f.text) || splitFitInside(want, f.raw):
			inside++
			misses = append(misses, name+" inside")
		default:
			misses = append(misses, name+" missing")
		}
	}
	return exact, run, inside, misses
}

func splitFitOne(want string, units []string) bool {
	for _, u := range units {
		if u == want {
			return true
		}
	}
	return false
}

func splitFitRun(want string, units []string) bool {
	for i := range units {
		acc := units[i]
		for j := i + 1; j < len(units) && strings.HasPrefix(want, acc) && len(acc) < len(want); j++ {
			acc += units[j]
			if acc == want {
				return true
			}
		}
	}
	return false
}

func splitFitInside(want string, units []string) bool {
	for _, u := range units {
		if strings.Contains(u, want) {
			return true
		}
	}
	return false
}

// Every P0-6 answer splits with the invariants, nothing dropped, no unit
// that is only links, and the counts the P0-6 statistics report (1,040
// units, at most 31 per answer) with the 20 bold numbered lines of four
// answers as items since 2026-10-31 (3 and 19 opencode, 4 claude, 13
// opencode): 1,060. Rows carry their header and nested items their parent,
// whose Text keeps its list number since 2026-11-01 (rsplit-15), so the
// count stays: the number goes with the item's first unit, never alone.
func TestSplitP06Answers(t *testing.T) {
	total, most := 0, 0
	for _, a := range p06Answers(t) {
		units, dropped := Split(a.AI, a.Answer, 0)
		splitCheck(t, a.AI, a.Answer, units, 0)
		if dropped != 0 {
			t.Errorf("%d-%s: %d dropped", a.Q, a.AI, dropped)
		}
		total += len(units)
		most = max(most, len(units))
		for _, u := range units {
			raw := SliceUTF16(a.Answer, u.Start, u.End)
			if !splitHasLetter(splitLinkRe.ReplaceAllString(splitDropNumber(raw), "")) {
				t.Errorf("%s (%d): only links: %q", u.ID, a.Q, raw)
			}
			if u.Kind == UnitRow && u.Context == "" {
				t.Errorf("%s (%d): a row without its header", u.ID, a.Q)
			}
		}
		splitNotFrom(t, a, units)
	}
	if total != 1060 || most != 31 {
		t.Errorf("%d units, at most %d per answer; want 1,060 and 31", total, most)
	}
	units, _ := Split("claude", p06FindAnswer(t, 19, "claude").Answer, 0)
	if u := units[1]; u.Kind != UnitItem || u.Text != "1. 수원 (화성행궁·수원화성)" {
		t.Errorf("ordered item %+v", u)
	}
	if u := units[2]; u.Kind != UnitItem || u.Context != "1. 수원 (화성행궁·수원화성)" || !strings.HasPrefix(u.Text, "이동:") {
		t.Errorf("nested item %+v", u)
	}
}

// splitNotFrom: no unit comes from a fenced code block, a heading line or
// a Sources block, found here line by line apart from the splitter.
func splitNotFrom(t *testing.T, a p06Answer, units []Unit) {
	t.Helper()
	type span struct{ a, b int }
	var out []span
	lines := strings.SplitAfter(a.Answer, "\n")
	pos, fence, sources := 0, false, false
	for _, l := range lines {
		s := strings.TrimSpace(l)
		start, end := UTF16Offset(a.Answer, pos), UTF16Offset(a.Answer, pos+len(l))
		pos += len(l)
		switch {
		case strings.HasPrefix(s, "```"):
			fence = !fence
			out = append(out, span{start, end})
		case fence, strings.HasPrefix(s, "#"), splitBoldHeading(s):
			out = append(out, span{start, end})
		case s == "Sources:":
			sources = true
			out = append(out, span{start, end})
		case sources && strings.HasPrefix(s, "- ["):
			out = append(out, span{start, end})
		default:
			sources = false
		}
	}
	for _, u := range units {
		for _, sp := range out {
			if u.Start < sp.b && sp.a < u.End {
				t.Errorf("%s (%d): unit %q in code, a heading or Sources", u.ID, a.Q, u.Text)
			}
		}
	}
}

// The P0-6 claims against the units (spec part 2, 'P0-6 맞춤'). The 10-31
// check, which split on lines without the heading rules, had 291 claims
// equal to one unit, 15 to a run of units and 22-claude3 inside one. Here
// 22-claude3 is a quote unit of its own, and the four claims of 19-opencode
// are lines that are only bold and start with a list number ("**1. 강릉
// (강원)**"): list items by the rule of part 2 (2026-10-31; as headings they
// were the four misses of 288/15/0), so 292 + 15. Units are compared
// without the list number an ordered item keeps (2026-11-01, rsplit-15):
// "1. 강릉 (강원)" is the claim "강릉 (강원)".
func TestSplitP06ClaimFit(t *testing.T) {
	exact, run, inside, misses := splitFitClaims(t)
	t.Logf("307 claims: %d equal one unit, %d a run of units, %d inside one; misses %v", exact, run, inside, misses)
	if exact != 292 || run != 15 || inside != 0 || len(misses) != 0 {
		t.Errorf("fit %d/%d/%d, misses %v; want 292/15/0 and none", exact, run, inside, misses)
	}
	units, _ := Split("opencode", p06FindAnswer(t, 19, "opencode").Answer, 0)
	if len(units) < 3 || units[1].Kind != UnitItem || units[1].Text != "1. 강릉 (강원)" || len(units[1].Before) != 0 || units[2].Context != "1. 강릉 (강원)" {
		t.Errorf("19-opencode: the bold line is not an item and the parent of the list after it: %+v", units[:min(3, len(units))])
	}
}

// splitTexts splits text, checks the invariants and gives the units' texts.
func splitTexts(t *testing.T, text string) ([]string, []Unit) {
	t.Helper()
	units, dropped := Split("a", text, 0)
	splitCheck(t, "a", text, units, 0)
	if dropped != 0 {
		t.Errorf("%q: %d dropped", text, dropped)
	}
	var texts []string
	for _, u := range units {
		texts = append(texts, u.Text)
	}
	return texts, units
}

func TestSplitSentences(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       []string
	}{
		{"Korean endings", "서울은 한국의 수도입니다. 부산은 어때요? 바다가 좋죠. 가 볼까? 네!",
			[]string{"서울은 한국의 수도입니다.", "부산은 어때요?", "바다가 좋죠.", "가 볼까?", "네!"}},
		{"numbers and versions", "금리는 3.75%입니다. Python 1.99.1이 나왔습니다. 약 8분 20초. 그다음입니다.",
			[]string{"금리는 3.75%입니다.", "Python 1.99.1이 나왔습니다.", "약 8분 20초.", "그다음입니다."}},
		{"abbreviations", "Use a tool, e.g. a linter. The U.S. Treasury sets it. Dr. Kim agrees, e.g., here. J. K. Rowling wrote it.",
			[]string{"Use a tool, e.g. a linter.", "The U.S. Treasury sets it.", "Dr. Kim agrees, e.g., here.", "J. K. Rowling wrote it."}},
		{"a capital before Korean", "원리부터 알고 싶다 → C. 다만 가혹합니다.", []string{"원리부터 알고 싶다 → C.", "다만 가혹합니다."}},
		{"Korean dates", "2026. 10. 30. 기준입니다. 다음입니다.", []string{"2026. 10. 30. 기준입니다.", "다음입니다."}},
		{"compact Korean dates", "2025.10.30. 기준 가격은 3만 원입니다. 정부는 2025.1.5. 발표했습니다. 버전 1.99.1. 다음입니다.",
			[]string{"2025.10.30. 기준 가격은 3만 원입니다.", "정부는 2025.1.5. 발표했습니다.", "버전 1.99.1.", "다음입니다."}},
		{"lower case next", "It costs approx. ten dollars. Next one.", []string{"It costs approx. ten dollars.", "Next one."}},
		{"brackets", "그는 (자세히 보면. 이렇다) 말했다. 그는 (정말?) 그렇다고 했다. 끝.",
			[]string{"그는 (자세히 보면. 이렇다) 말했다.", "그는 (정말?) 그렇다고 했다.", "끝."}},
		{"quotes", `He said "Stop. Now." Then he left. 그는 '가자. 어서' 했다.`,
			[]string{`He said "Stop. Now."`, "Then he left.", "그는 '가자. 어서' 했다."}},
		{"apostrophes are not quotes", "It's Katz's place. Don't go.", []string{"It's Katz's place.", "Don't go."}},
		{"code, math and links", "Run `a. b` now. 넓이는 $x. y$ 이다. See [the docs. Here](https://a.example/x.y) now. Done.",
			[]string{"Run a. b now.", "넓이는 $x. y$ 이다.", "See the docs. Here now.", "Done."}},
		{"bold across sentences", "**첫 문장입니다. 둘째 문장입니다.** 셋째입니다.", []string{"첫 문장입니다.", "둘째 문장입니다.", "셋째입니다."}},
		// The app pairs the two runs across the sentences.
		{"bold across sentences, next to Hangul", "**첫 문장입니다. 둘째**는 이렇다. 이것은**중요합니다. 다음**도.",
			[]string{"첫 문장입니다.", "둘째는 이렇다.", "이것은중요합니다.", "다음도."}},
		{"marks after the end", "끝났습니다.)** 다음.[^1] 그다음.[2] 마지막!?", []string{"끝났습니다.)", "다음.", "그다음.[2]", "마지막!?"}},
		{"numbering in a sentence", "단계는 다음과 같다. 1. 준비한다. 2. 실행한다.", []string{"단계는 다음과 같다.", "1. 준비한다.", "2. 실행한다."}},
		{"ellipsis and CJK stops", "그런데… 아니다. 好。 Really?! Yes.", []string{"그런데…", "아니다.", "好。", "Really?!", "Yes."}},
		{"soft line breaks join", "첫 줄이\n이어집니다. 둘째\n문장.", []string{"첫 줄이 이어집니다.", "둘째 문장."}},
		// A backslash in code is only a backslash, so the code ends there.
		{"a backslash before the closing backtick", "Windows 경로 구분자는 `\\`입니다. macOS는 `/`입니다.",
			[]string{"Windows 경로 구분자는 \\입니다.", "macOS는 /입니다."}},
		{"Windows paths in code", "`C:\\`에 설치합니다. **중요**: `D:\\`는 비워 둡니다.", []string{"C:\\에 설치합니다.", "중요: D:\\는 비워 둡니다."}},
		// A lower-case word after a Korean sentence starts one; only an
		// English word before the period may be an abbreviation.
		{"lower case after Korean", "가장 최신 모델은 아이폰 듀오입니다. iPhone 18 Pro는 9월 19일에 나왔습니다. 끝.",
			[]string{"가장 최신 모델은 아이폰 듀오입니다.", "iPhone 18 Pro는 9월 19일에 나왔습니다.", "끝."}},
		{"git after Korean", "merge는 이력을 보존합니다. git rebase는 이력을 다시 씁니다.", []string{"merge는 이력을 보존합니다.", "git rebase는 이력을 다시 씁니다."}},
		{"a capital that is a word", "I recommend plan A. It is cheaper. John F. Kennedy said so.",
			[]string{"I recommend plan A.", "It is cheaper.", "John F. Kennedy said so."}},
		// An initial after a lower-case word or a title: another initial
		// or a title tells it from a word.
		{"initials after a word or a title",
			"Harry Potter was written by J. K. Rowling. It sold well. The economist J. M. Keynes said so. Dr. J. Smith agrees. Mrs. J. K. Rowling wrote it. See Fig. A. It shows how.",
			[]string{"Harry Potter was written by J. K. Rowling.", "It sold well.", "The economist J. M. Keynes said so.", "Dr. J. Smith agrees.",
				"Mrs. J. K. Rowling wrote it.", "See Fig. A.", "It shows how."}},
		{"initials before Korean", "해리 포터는 J. K. 롤링이 썼습니다. 다음입니다.", []string{"해리 포터는 J. K. 롤링이 썼습니다.", "다음입니다."}},
		{"inch marks", `The 6.1" model is small. The next one is big. The 6.7" model is large.`,
			[]string{`The 6.1" model is small.`, "The next one is big.", `The 6.7" model is large.`}},
		// Only "1." starts a list inside a paragraph, as in the app.
		{"a Korean date at a line start", "아이폰 17 출시일은\n10. 3.에 공개됐습니다.", []string{"아이폰 17 출시일은 10. 3.에 공개됐습니다."}},
		{"a number other than 1 starts no list", "설명 문단\n2. 둘째 줄입니다.", []string{"설명 문단 2. 둘째 줄입니다."}},
		// [t][ref] needs a definition: citations and indexes stay as written.
		{"citations and indexes", "근거가 있습니다[1][2]. 행렬 A[0][1] 값은 3입니다.", []string{"근거가 있습니다[1][2].", "행렬 A[0][1] 값은 3입니다."}},
		// An ordered item's first unit keeps its number, and the sentence is
		// read from its words: no period of the number ends it, and an
		// initial or a date right after it is read as without it.
		{"an item's number starts its first sentence",
			"1. 서울입니다. 둘째.\n2) 부산입니다. 셋째.\n10. 열 번째입니다. 끝.\n100. 백 번째. 끝.\n3. J. Smith wrote it. Next.\n4. 2026. 10. 30. 기준입니다. 끝.\n5. 3.75%입니다. 끝.",
			[]string{"1. 서울입니다.", "둘째.", "2) 부산입니다.", "셋째.", "10. 열 번째입니다.", "끝.", "100. 백 번째.", "끝.",
				"3. J. Smith wrote it.", "Next.", "4. 2026. 10. 30. 기준입니다.", "끝.", "5. 3.75%입니다.", "끝."}},
		// 。！？ end a sentence with nothing after them (2026-11-01,
		// rsplit-14), closers included, but not a quote the words go on after.
		{"Japanese sentences", "東京は日本の首都です。人口は約1400万人です！本当ですか？はい。",
			[]string{"東京は日本の首都です。", "人口は約1400万人です！", "本当ですか？", "はい。"}},
		{"Chinese sentences", "北京是中国的首都。人口约2100万！真的吗？是的。",
			[]string{"北京是中国的首都。", "人口约2100万！", "真的吗？", "是的。"}},
		{"CJK closers and quotes", "彼は言った。「行こう。」そして去った。（注：古い話です。）次の文です。他说：“走吧！”然后离开了。",
			[]string{"彼は言った。", "「行こう。」そして去った。", "（注：古い話です。）次の文です。", "他说：“走吧！”然后离开了。"}},
		// A straight quote right after the mark opens the next sentence.
		{"CJK straight quotes", `他说。"我们走吧。"然后离开了。`, []string{"他说。", `"我们走吧。"然后离开了。`}},
		{"CJK marks in code, math and links", "`a。b`を使います。$x。y$です。[文書。ここ](https://a.example)です。",
			[]string{"a。bを使います。", "$x。y$です。", "文書。ここです。"}},
		{"CJK stops keep other languages", "好。 Really?! Yes. 서울입니다。다음입니다.",
			[]string{"好。", "Really?!", "Yes.", "서울입니다。", "다음입니다."}},
		// 。！？ end a sentence after other marks too ("…。", "!？").
		{"CJK stops after other marks", "そうですね…。次の点です。他犹豫了……。然后离开了。本当!？次です。",
			[]string{"そうですね…。", "次の点です。", "他犹豫了……。", "然后离开了。", "本当!？", "次です。"}},
		// A number other than 1 does not start a list in a quote's paragraph either.
		{"a quote's paragraph goes on over a number", "> 출시일은\n> 10. 3.에 공개됩니다.\n\n> 설명입니다\n> 2. 둘째입니다.",
			[]string{"출시일은 10. 3.에 공개됩니다.", "설명입니다 2. 둘째입니다."}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, _ := splitTexts(t, c.text)
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestSplitBlocks(t *testing.T) {
	type want struct {
		kind, text, context, before string
	}
	for _, c := range []struct {
		name, text string
		want       []want
	}{
		{"headings, code and formulas are context",
			"## 제목\n\n본문입니다.\n\n```go\nx := 1\n```\n\n**요약**\n다음 문장입니다.\n\n식:\n$$\nx^2 + y^2 = z^2\n$$\n\n\\[\nE = mc^2\n\\]\n\n끝.",
			[]want{{UnitSentence, "본문입니다.", "", "## 제목"}, {UnitSentence, "다음 문장입니다.", "", "(code, not judged) x := 1|**요약**"},
				{UnitSentence, "식:", "", ""}, {UnitSentence, "끝.", "", "(formula, not judged) x^2 + y^2 = z^2|(formula, not judged) E = mc^2"}}},
		{"setext heading", "제목\n===\n\n본문.", []want{{UnitSentence, "본문.", "", "제목"}}},
		{"a bold sentence or a long bold line is a unit", "**이건 문장입니다.**\n\n**" + strings.Repeat("가", 41) + "**",
			[]want{{UnitSentence, "이건 문장입니다.", "", ""}, {UnitSentence, strings.Repeat("가", 41), "", ""}}},
		{"sources and links only go",
			"본문입니다. [공식 사이트](https://a.example/p)\n\n([출처](https://b.example))\n\nSources:\n- [A](https://a.example)\n\n- <https://b.example>\n\n**출처:** [C](https://c.example), [D](https://d.example)\n\n## 참고 자료\n- [E](https://e.example)\n\n뒤 문단입니다.",
			[]want{{UnitSentence, "본문입니다.", "", ""}, {UnitSentence, "뒤 문단입니다.", "", ""}}},
		{"rules, comments and definitions go",
			"앞 문장.[^1]\n\n---\n\n<!-- 숨김\n여러 줄 -->\n\n[1]: https://a.example\n[^1]: 각주 내용\n    이어짐\n\n뒤 문장.",
			[]want{{UnitSentence, "앞 문장.", "", ""}, {UnitSentence, "뒤 문장.", "", ""}}},
		{"tables", "| 항목 | HTTP |\n|---|:---:|\n| **포트** | 80 |\n| 암호화 | 없음 \\| 평문 |\n|  |  |\n\n뒤.",
			[]want{{UnitRow, "포트 | 80", "항목 | HTTP", ""}, {UnitRow, "암호화 | 없음 | 평문", "항목 | HTTP", ""}, {UnitSentence, "뒤.", "", ""}}},
		// A cell's "\|" is "|" in its code and math too, and the cells past
		// the header's are not shown, as in GFM and the app; in a quote too.
		{"escaped pipes and cells past the header",
			"| `a \\| b` | 파이프 |\n|---|---|\n| `c \\| d` | $\\|x\\|$ |\n| 절댓값 | $|x|$ |\n\n> | 기호 | 뜻 |\n> |---|---|\n> | 절댓값 | $|x|$ |",
			[]want{{UnitRow, "c | d | $|x|$", "a | b | 파이프", ""}, {UnitRow, "절댓값 | $", "a | b | 파이프", ""}, {UnitRow, "절댓값 | $", "기호 | 뜻", ""}}},
		{"nested lists", "1. **수원**\n   - 이동: 1시간. 가깝습니다.\n   - [ ] 볼거리: 화성\n2. 춘천\n이어지는 줄입니다.\n\n   둘째 문단.\n\n본문.",
			[]want{{UnitItem, "1. 수원", "", ""}, {UnitItem, "이동: 1시간.", "1. 수원", ""}, {UnitItem, "가깝습니다.", "1. 수원", ""}, {UnitItem, "볼거리: 화성", "1. 수원", ""},
				{UnitItem, "2. 춘천 이어지는 줄입니다.", "", ""}, {UnitItem, "둘째 문단.", "", ""}, {UnitSentence, "본문.", "", ""}}},
		{"quotes", "> 인용 첫 문장. 둘째.\n> 이어진 줄.\n>\n> - 목록 인용\n\n> **굵은 제목**\n> 본문.",
			[]want{{UnitQuote, "인용 첫 문장.", "", ""}, {UnitQuote, "둘째.", "", ""}, {UnitQuote, "이어진 줄.", "", ""}, {UnitQuote, "목록 인용", "", ""},
				{UnitQuote, "본문.", "", "**굵은 제목**"}}},
		{"details", "<details>\n<summary>펼치기</summary>\n\n숨은 **내용**입니다.\n\n</details>",
			[]want{{UnitSentence, "숨은 내용입니다.", "", "펼치기"}}},
		{"indented code and an unclosed fence", "본문.\n\n    code line\n\n```\nnever closed",
			[]want{{UnitSentence, "본문.", "", ""}}},
		{"an entity in code or after a backslash stays as written",
			"HTML에서 앰퍼샌드는 `&amp;`로 씁니다.\n\n- `&lt;` and `&amp;` stay.\n\n| entity | char |\n|---|---|\n| `&amp;` | &amp; |\n\nEscaped \\&amp; too.",
			[]want{{UnitSentence, "HTML에서 앰퍼샌드는 &amp;로 씁니다.", "", ""}, {UnitItem, "&lt; and &amp; stay.", "", ""},
				{UnitRow, "&amp; | &", "entity | char", ""}, {UnitSentence, "Escaped &amp; too.", "", ""}}},
		{"a quote ends a table", "| a | b |\n|---|---|\n| 1 | 2 |\n> 인용입니다.\n이어지는 줄 | 입니다.",
			[]want{{UnitRow, "1 | 2", "a | b", ""}, {UnitQuote, "인용입니다.", "", ""}, {UnitQuote, "이어지는 줄 | 입니다.", "", ""}}},
		// A line with a link that reads as a sentence of its own is a unit
		// in a Sources block; source lines stay out.
		{"a sentence with a citation in a Sources block is a unit",
			"2026년 최저임금은 시간당 10,320원입니다.\n\n**참고**\n- 월 환산액은 2,156,880원입니다([고용노동부](https://www.moel.go.kr)).\n- 주휴수당 포함 기준입니다.\n\n" +
				"Source:\n- It was completed in 2010 ([CTBUH](https://www.ctbuh.org)).\n\n**참고:**\n자세한 기준은 [국세청 안내](https://www.nts.go.kr)에서 확인하세요.\n\n" +
				"## References\n- [Title](https://a.example)\n- 통계청, 2025 인구주택총조사 (https://kostat.go.kr)\n- Burj Khalifa opened in 2010, see https://example.com.",
			[]want{{UnitSentence, "2026년 최저임금은 시간당 10,320원입니다.", "", ""}, {UnitItem, "월 환산액은 2,156,880원입니다(고용노동부).", "", ""},
				{UnitItem, "주휴수당 포함 기준입니다.", "", ""}, {UnitItem, "It was completed in 2010 (CTBUH).", "", ""},
				{UnitSentence, "자세한 기준은 국세청 안내에서 확인하세요.", "", ""}, {UnitItem, "Burj Khalifa opened in 2010, see https://example.com.", "", ""}}},
		// The block goes on after such a line, a source's date at the end
		// is no sentence end, and a source line ends what the sentence line
		// opened (no unit takes in "[고용노동부](…)").
		{"source lines after a sentence line stay out",
			"인구는 약 5,168만 명입니다.\n\n**출처**\n- [고용노동부 보도자료](https://www.moel.go.kr) (2025.8.5.)\n- 통계청, 2025 인구주택총조사 (https://kostat.go.kr)\n" +
				"- [최저임금위원회](https://www.minimumwage.go.kr), 2025.7.11.\n\n" +
				"**Sources**\n- \"Burj Khalifa.\" Wikipedia, 2024, https://en.wikipedia.org/wiki/Burj_Khalifa.\n- CTBUH, Tall Buildings Database (https://www.ctbuh.org)\n\n" +
				"**참고**\n월 환산액은 2,156,880원입니다([고용노동부](https://www.moel.go.kr)).\n[고용노동부](https://www.moel.go.kr)\n주휴수당 포함 기준입니다.",
			[]want{{UnitSentence, "인구는 약 5,168만 명입니다.", "", ""}, {UnitItem, "\"Burj Khalifa.\"", "", ""},
				{UnitItem, "Wikipedia, 2024, https://en.wikipedia.org/wiki/Burj_Khalifa.", "", ""},
				{UnitSentence, "월 환산액은 2,156,880원입니다(고용노동부).", "", ""}, {UnitSentence, "주휴수당 포함 기준입니다.", "", ""}}},
		{"a blank line in a Sources block ends a sentence line's paragraph",
			"**참고**\n월 환산액은 2,156,880원입니다([고용노동부](https://www.moel.go.kr)).\n\n    code line\n\n끝.",
			[]want{{UnitSentence, "월 환산액은 2,156,880원입니다(고용노동부).", "", ""}, {UnitSentence, "끝.", "", "(code, not judged) code line"}}},
		// A later sentence of an item or a quote shows what only looks like
		// a list marker, as the app shows it in the item.
		{"marker-like text in an item or a quote stays",
			"- 순서는 다음과 같다. 1. 준비한다. 2. 실행한다.\n\n> 순서는 다음과 같다. 1. 준비한다. - 실행한다.",
			[]want{{UnitItem, "순서는 다음과 같다.", "", ""}, {UnitItem, "1. 준비한다.", "", ""}, {UnitItem, "2. 실행한다.", "", ""},
				{UnitQuote, "순서는 다음과 같다.", "", ""}, {UnitQuote, "1. 준비한다.", "", ""}, {UnitQuote, "- 실행한다.", "", ""}}},
		// A definition needs a destination; the app shows what follows a
		// comment's end.
		{"a label and words is a sentence; text after a comment shows",
			"[Note]: The deadline moved to March.\n\n[참고]: 이 값은 2026년 기준입니다.\n\n<!--\n숨김\n--> 보이는 문장입니다.\n\n[1]: https://a.example \"Title\"\n\n다음 문단.",
			[]want{{UnitSentence, "[Note]: The deadline moved to March.", "", ""}, {UnitSentence, "[참고]: 이 값은 2026년 기준입니다.", "", ""},
				{UnitSentence, "보이는 문장입니다.", "", ""}, {UnitSentence, "다음 문단.", "", ""}}},
		// Indented code inside a list, and code and tables inside a quote.
		{"code in lists and quotes is context",
			"Run this:\n\n    - name: build\n      run: make\n\nDone.\n\n- 설치합니다.\n\n        pip install foo\n\n- 다음 단계.\n\n" +
				"> ```\n> code\n> ```\n> 본문.\n\n> | a | b |\n> |---|---|\n> | 1 | 2 |\n\n> 명령은 `git\n> log` 입니다.",
			[]want{{UnitSentence, "Run this:", "", ""}, {UnitSentence, "Done.", "", "(code, not judged) - name: build\nrun: make"},
				{UnitItem, "설치합니다.", "", ""}, {UnitItem, "다음 단계.", "", "(code, not judged) pip install foo"},
				{UnitQuote, "본문.", "", "(code, not judged) code"}, {UnitRow, "1 | 2", "a | b", ""}, {UnitQuote, "명령은 git log 입니다.", "", ""}}},
		// A line that is only bold and starts with a list number is an item
		// (2026-10-31 user decision, P0-6 19-opencode), and the list right
		// after it is its children, over blank lines, until a paragraph.
		{"bold numbered lines are items, parents of the list after them",
			"소개합니다.\n\n**1. 강릉 (강원)**\n- 이동: KTX 2시간\n- 팁: 저녁 기차.\n  - 막차 확인\n\n**2. 전주 (전북)**:\n\n- 이동: KTX 1시간 40분\n\n정리하면 둘 다 좋습니다.\n- 따로 쓴 목록",
			[]want{{UnitSentence, "소개합니다.", "", ""}, {UnitItem, "1. 강릉 (강원)", "", ""}, {UnitItem, "이동: KTX 2시간", "1. 강릉 (강원)", ""},
				{UnitItem, "팁: 저녁 기차.", "1. 강릉 (강원)", ""}, {UnitItem, "막차 확인", "팁: 저녁 기차.", ""}, {UnitItem, "2. 전주 (전북)", "", ""},
				{UnitItem, "이동: KTX 1시간 40분", "2. 전주 (전북)", ""}, {UnitSentence, "정리하면 둘 다 좋습니다.", "", ""}, {UnitItem, "따로 쓴 목록", "", ""}}},
		{"a paragraph or a heading after a bold numbered line ends its list",
			"**1. 유산균의 작용**\n김치를 담그면 유산균이 증식합니다.\n- 초기: 톡 쏩니다.\n\n**2. pH 하락**\n## 다음\n- 항목",
			[]want{{UnitItem, "1. 유산균의 작용", "", ""}, {UnitSentence, "김치를 담그면 유산균이 증식합니다.", "", ""}, {UnitItem, "초기: 톡 쏩니다.", "", ""},
				{UnitItem, "2. pH 하락", "", ""}, {UnitItem, "항목", "", "## 다음"}}},
		{"long, sentence and quoted bold numbered lines are items too",
			"**1. 공복 상태에서 지방 산화율이 높아지는 것은 사실이며 연구들도 대체로 그렇게 봅니다**\n\n__2) 운동 전 식사는 수행 능력을 높입니다.__\n\n> **3. 인용 안의 번호 줄**\n> 본문.",
			[]want{{UnitItem, "1. 공복 상태에서 지방 산화율이 높아지는 것은 사실이며 연구들도 대체로 그렇게 봅니다", "", ""},
				{UnitItem, "2) 운동 전 식사는 수행 능력을 높입니다.", "", ""}, {UnitQuote, "3. 인용 안의 번호 줄", "", ""}, {UnitQuote, "본문.", "", ""}}},
		{"bold lines without a list number stay headings",
			"**장점**\n- 빠릅니다.\n\n**10. 30. 기준**\n본문입니다.\n\n**1.강릉**\n끝.",
			[]want{{UnitItem, "빠릅니다.", "", "**장점**"}, {UnitSentence, "본문입니다.", "", "**10. 30. 기준**"}, {UnitSentence, "끝.", "", "**1.강릉**"}}},
		// s1-bold review: the probes behind each fix.
		{"a list indented four columns under a bold line is its children, not code",
			"**1. 강릉**\n    - 이동: KTX\n\t- 팁: 저녁",
			[]want{{UnitItem, "1. 강릉", "", ""}, {UnitItem, "이동: KTX", "1. 강릉", ""}, {UnitItem, "팁: 저녁", "1. 강릉", ""}}},
		{"a bold number before more words is no sentence of its own",
			"**1. 강릉 (강원)**: 바다가 유명합니다.\n\n**2.** 둘째 이유입니다.",
			[]want{{UnitSentence, "1. 강릉 (강원): 바다가 유명합니다.", "", ""}, {UnitSentence, "2. 둘째 이유입니다.", "", ""}}},
		{"a heading inside an item keeps the bold line the parent of its list",
			"**1. 강릉**\n- 이동: KTX\n  **추천 코스**\n  - 안목해변\n- 팁: 막차",
			[]want{{UnitItem, "1. 강릉", "", ""}, {UnitItem, "이동: KTX", "1. 강릉", ""}, {UnitItem, "안목해변", "이동: KTX", "**추천 코스**"}, {UnitItem, "팁: 막차", "1. 강릉", ""}}},
		{"a bold numbered line inside an item is the parent of the list after it",
			"- 여행지\n\n  **1. 강릉**\n  - 이동: KTX\n\n  **2. 전주**\n  - 이동: 버스",
			[]want{{UnitItem, "여행지", "", ""}, {UnitItem, "1. 강릉", "여행지", ""}, {UnitItem, "이동: KTX", "1. 강릉", ""}, {UnitItem, "2. 전주", "여행지", ""}, {UnitItem, "이동: 버스", "2. 전주", ""}}},
		{"a quote after the list ends the bold line's list",
			"**1. 강릉**\n- 이동: KTX\n\n> 인용입니다.\n\n- 다른 목록",
			[]want{{UnitItem, "1. 강릉", "", ""}, {UnitItem, "이동: KTX", "1. 강릉", ""}, {UnitQuote, "인용입니다.", "", ""}, {UnitItem, "다른 목록", "", ""}}},
		{"a dotted date with its weekday, an underlined line and a numbered Sources label are no items",
			"**10. 30.(목) 기준**\n- 가격: 1만 원\n\n**1. 강릉**\n---\n- 이동\n\n**5. 출처**\n- 통계청, 2025 (https://kostat.go.kr)",
			[]want{{UnitItem, "가격: 1만 원", "", "**10. 30.(목) 기준**"}, {UnitItem, "이동", "", "**1. 강릉**"}}},
		// An ordered item keeps its number in its first unit, task box and
		// all, in a quote too; a bullet does not (2026-11-01, rsplit-15).
		// A number is no content: an item that is only a link is no unit.
		{"ordered items keep their number",
			"1. 서울\n2) 부산입니다. 둘째 문장.\n10. 마지막\n    - 하위 항목\n\n- 점 목록\n- [ ] 할 일\n\n1. [x] 완료\n2. [통계청](https://kostat.go.kr)\n\n> 1. 인용 번호. 둘째.\n> - 인용 점",
			[]want{{UnitItem, "1. 서울", "", ""}, {UnitItem, "2) 부산입니다.", "", ""}, {UnitItem, "둘째 문장.", "", ""}, {UnitItem, "10. 마지막", "", ""},
				{UnitItem, "하위 항목", "10. 마지막", ""}, {UnitItem, "점 목록", "", ""}, {UnitItem, "할 일", "", ""}, {UnitItem, "1. [x] 완료", "", ""},
				{UnitQuote, "1. 인용 번호.", "", ""}, {UnitQuote, "둘째.", "", ""}, {UnitQuote, "인용 점", "", ""}}},
		// A bold numbered line inside the list under another keeps the outer
		// one the parent of its own list.
		{"a bold numbered line under another",
			"**1. 1일차**\n- 오전\n  **1) 강릉역 도착**\n  - 커피를 마십니다.\n- 오후: 해변을 걷습니다.",
			[]want{{UnitItem, "1. 1일차", "", ""}, {UnitItem, "오전", "1. 1일차", ""}, {UnitItem, "1) 강릉역 도착", "오전", ""},
				{UnitItem, "커피를 마십니다.", "1) 강릉역 도착", ""}, {UnitItem, "오후: 해변을 걷습니다.", "1. 1일차", ""}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, units := splitTexts(t, c.text)
			var got, exp []want
			for _, u := range units {
				got = append(got, want{u.Kind, u.Text, u.Context, strings.Join(u.Before, "|")})
			}
			exp = c.want
			if fmt.Sprint(got) != fmt.Sprint(exp) {
				t.Errorf("got  %q\nwant %q", got, exp)
			}
		})
	}
}

func TestStrip(t *testing.T) {
	for _, c := range []struct{ kind, raw, want string }{
		{UnitSentence, "**굵게** 와 *기울임* 과 _밑줄_ ~~취소~~ 1~10분", "굵게 와 기울임 과 밑줄 취소 1~10분"},
		{UnitSentence, "snake_case_name, __init__, 2 * 3, a*b*c", "snake_case_name, init, 2 * 3, abc"},
		{UnitSentence, "`a  *b*` and ``x ` y``", "a *b* and x ` y"},
		{UnitSentence, "[문서](https://x.example/a_(b)) ![그림](a.png) <https://b.example> [ref][1] [plain]", "문서 그림 https://b.example ref [plain]"},
		{UnitSentence, "근거[^1] 있음<!-- 주석 --> 끝", "근거 있음 끝"},
		{UnitSentence, "<b>굵게</b><br>다음 List<T> <a href=\"https://x\">링크</a>", "굵게 다음 List<T> 링크"},
		{UnitSentence, "&amp; &lt;b&gt; &nbsp;x &#8212; &#x2014; &#0; &unknown; &constructor;", "& <b> x — — \xef\xbf\xbd &unknown; &constructor;"},
		{UnitSentence, `\*별\* \[옵션\] \\ 끝`, `*별* [옵션] \ 끝`},
		{UnitSentence, "넓이 $x_1 * y_2$ 끝 *강조* \\(a_1\\)", "넓이 $x_1 * y_2$ 끝 강조 \\(a_1\\)"},
		{UnitSentence, "  여러\n  줄의\t공백  ", "여러 줄의 공백"},
		{UnitSentence, "- 문장의 목록 표시는 그대로", "- 문장의 목록 표시는 그대로"},
		// A bullet's span starts after its marker and task box and an
		// ordered item's at its number, so what looks like a marker at the
		// start of a span is the item's own text, shown as written.
		{UnitItem, "- 실행한다.", "- 실행한다."},
		{UnitItem, "3. 셋째", "3. 셋째"},
		{UnitItem, "[x] 완료", "[x] 완료"},
		{UnitQuote, "> 첫 줄\n> > 둘째 줄", "첫 줄 둘째 줄"},
		{UnitQuote, "명령은 `git\n> log` 이고 $a +\n> b$ 입니다", "명령은 git log 이고 $a + b$ 입니다"},
		// In code a backslash is only a backslash; out of code it escapes.
		{UnitSentence, "`C:\\`에 설치합니다. **중요**: `D:\\`는 비워 둡니다.", "C:\\에 설치합니다. 중요: D:\\는 비워 둡니다."},
		{UnitSentence, "설정은 `%APPDATA%\\Asksemble\\` 폴더에 있습니다.", "설정은 %APPDATA%\\Asksemble\\ 폴더에 있습니다."},
		{UnitSentence, "\\`not code` here", "`not code` here"},
		{UnitSentence, "\\``x` y", "`x y"},
		// Runs of "*" and "~" that pair with nothing are shown, as the app
		// shows them; two inside words pair.
		{UnitSentence, "파이썬에서 2**10은 1024입니다.", "파이썬에서 2**10은 1024입니다."},
		{UnitSentence, "2 ** 10 = 1024입니다.", "2 ** 10 = 1024입니다."},
		{UnitSentence, "범위는 1~~2입니다.", "범위는 1~~2입니다."},
		{UnitSentence, "a***b 입니다.", "a***b 입니다."},
		{UnitSentence, "2**10 and 3**2", "210 and 32"},
		// Next to Hangul a lone run ends or starts bold another sentence holds.
		{UnitSentence, "둘째**는 이렇다.", "둘째는 이렇다."},
		{UnitSentence, "iPhone**은 좋다.", "iPhone은 좋다."},
		// [t][ref] needs a definition Strip cannot see.
		{UnitSentence, "근거가 있습니다[1][2].", "근거가 있습니다[1][2]."},
		{UnitSentence, "행렬 A[0][1] 값은 3입니다.", "행렬 A[0][1] 값은 3입니다."},
		{UnitRow, "| **a** | b \\| c |  | `d|e` |", "a | b | c | | `d | e`"}, // GFM cuts cells at a "|" in code too
		{UnitRow, "x | y", "x | y"},
	} {
		if got := Strip(c.kind, c.raw); got != c.want {
			t.Errorf("Strip(%s, %q) = %q, want %q", c.kind, c.raw, got, c.want)
		}
	}
}

// Offsets count UTF-16 code units of the text as given: a BOM, "\r\n",
// leading spaces, Hangul and emoji are not rewritten or skipped over.
func TestSplitOffsets(t *testing.T) {
	text := "\xef\xbb\xbf  😀 첫 문장입니다.\r\n둘째 줄이에요.\r\n\r\n- 항목 🙂 하나\r\n- 항목 둘\r\n"
	got, units := splitTexts(t, text)
	want := []string{"😀 첫 문장입니다.", "둘째 줄이에요.", "항목 🙂 하나", "항목 둘"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	// BOM 1 + 2 spaces = 3; the emoji counts 2 and "첫 문장입니다." 8
	if units[0].Start != 3 || units[0].End != 3+2+1+8 {
		t.Errorf("first unit %d..%d", units[0].Start, units[0].End)
	}
	for _, u := range units {
		if raw := SliceUTF16(text, u.Start, u.End); strings.ContainsAny(raw, "\r\n") {
			t.Errorf("%s: raw %q holds a line break", u.ID, raw)
		}
	}
}

// A Japanese paragraph of 40 sentences and no spaces is 40 units, each a
// whole sentence (2026-11-01, rsplit-14). Before, it was one paragraph
// over MaxUnitChars, cut at the limit inside a word.
func TestSplitJapaneseParagraph(t *testing.T) {
	var text strings.Builder
	var want []string
	for i := 1; i <= 40; i++ {
		s := fmt.Sprintf("第%d文では、東京の交通機関について詳しく説明します。", i)
		text.WriteString(s)
		want = append(want, s)
	}
	if n := utf8.RuneCountInString(text.String()); n <= MaxUnitChars {
		t.Fatalf("%d characters: not longer than MaxUnitChars", n)
	}
	got, units := splitTexts(t, text.String())
	if strings.Join(got, "\n") != strings.Join(want, "\n") || len(units) != 40 || !units[39].Judged {
		t.Errorf("%d units: %q", len(units), got)
	}
}

// A unit's Math is where its Text has math the answer drew as math
// (2026-11-01, rsplit-7), in UTF-16 offsets into Text: not code and not an
// escaped "$", whose marks Text has lost.
func TestSplitMath(t *testing.T) {
	type math struct {
		text    string
		display bool
	}
	for _, c := range []struct {
		name, text string
		want       [][]math // per unit
	}{
		{"code", "Run `export PATH=$HOME/bin:$PATH` first.", [][]math{nil}},
		{"escaped dollars", `변수는 \$HOME/\$USER 입니다.`, [][]math{nil}},
		{"math in code", "`$x$`", [][]math{nil}},
		{"after bold", "**5**$x$ 입니다.", [][]math{{{"$x$", false}}}},
		{"dollars and parentheses", `넓이는 $\pi r^2$이고 \(\alpha\)도 씁니다.`, [][]math{{{`$\pi r^2$`, false}, {`\(\alpha\)`, false}}}},
		{"display in a line", `식은 $$E = mc^2$$ 와 \[x^2\] 입니다. 둘째 $y$.`, [][]math{{{"$$E = mc^2$$", true}, {`\[x^2\]`, true}}, {{"$y$", false}}}},
		{"rows", "| 식 | 값 | 코드 | 끝 |\n|---|---|---|---|\n|  | $a^2$ | `$b$` | $c$ |", [][]math{{{"$a^2$", false}, {"$c$", false}}}},
		{"cells past the header's", "| 식 | 값 |\n|---|---|\n|  | $a^2$ | `$b$` | $c$ |\n| 절댓값 | $|x|$ |", [][]math{{{"$a^2$", false}}, nil}},
		{"a quote's lines", "> 인용 $a +\n> b$ 입니다.", [][]math{{{"$a + b$", false}}}},
		{"an item and a link", "1. 😀 $x_1$ 과 [그림 $y$](https://a.example).", [][]math{{{"$x_1$", false}, {"$y$", false}}}},
		{"money", "가격은 $5에서 $10 사이입니다.", [][]math{nil}},
		{"escaped pipes in a row", "| a | b |\n|---|---|\n| `c \\| d` | $\\|x\\|$ |", [][]math{{{"$|x|$", false}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, units := splitTexts(t, c.text)
			var got [][]math
			for _, u := range units {
				var ms []math
				for _, m := range u.Math {
					ms = append(ms, math{SliceUTF16(u.Text, m.Start, m.End), m.Display})
				}
				got = append(got, ms)
			}
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("got  %v\nwant %v", got, c.want)
			}
		})
	}
	// Offsets count UTF-16 code units of Text: "5$x$" and "1. 😀 $x_1$".
	if units, _ := Split("a", "**5**$x$ 입니다.", 0); len(units) != 1 || fmt.Sprint(units[0].Math) != "[{1 4 false}]" {
		t.Errorf("after bold: %+v", units)
	}
	if units, _ := Split("a", "1. 😀 $x_1$ 과 [그림 $y$](https://a.example).", 0); len(units) != 1 || fmt.Sprint(units[0].Math) != "[{6 11 false} {17 20 false}]" {
		t.Errorf("an item and a link: %+v", units)
	}
}

// Unit.Math is a field added inside /v1: a unit without math has no key.
func TestSplitMathJSON(t *testing.T) {
	units, _ := Split("claude", "넓이는 $x^2$입니다. 끝.", 0)
	if len(units) != 2 {
		t.Fatalf("%d units", len(units))
	}
	var got []string
	for _, u := range units {
		raw, err := json.Marshal(u)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(raw))
	}
	want := []string{`{"id":"claude:1","n":1,"kind":"sentence","text":"넓이는 $x^2$입니다.","start":0,"end":13,"judged":true,"math":[{"start":4,"end":9,"display":false}]}`,
		`{"id":"claude:2","n":2,"kind":"sentence","text":"끝.","start":14,"end":16,"judged":true}`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("JSON\n got %s\nwant %s", got, want)
	}
}

// Units after the first maxUnits are kept but not judged.
func TestSplitUnitCap(t *testing.T) {
	text := strings.Repeat("문장입니다. ", 45)
	units, _ := Split("x", text, 0)
	if len(units) != 45 || !units[39].Judged || units[40].Judged || units[44].Judged {
		t.Fatalf("%d units", len(units))
	}
	splitCheck(t, "x", text, units, 0)
	units, _ = Split("x", text, 3)
	splitCheck(t, "x", text, units, 3)
}

// A unit over MaxUnitChars characters is cut at its last "; " or ", ",
// else after its last 、，or ；, else at its last space, else at the limit,
// never inside code.
func TestSplitLongUnit(t *testing.T) {
	part := strings.Repeat("가", 95) + ", "
	for _, c := range []struct {
		name, text string
		ends       string
	}{
		{"commas", strings.Repeat(part, 13) + "끝.", ","},
		{"spaces", strings.Repeat(strings.Repeat("가", 99)+" ", 13) + "끝.", ""},
		{"no space", strings.Repeat("가", 1500), ""},
		{"code", "앞 `" + strings.Repeat("a, ", 300) + "` " + strings.Repeat(part, 4), ""},
		// A space after each 、 comes later in its part, so a cut at the last
		// space would not end with 、.
		{"Japanese commas before spaces", strings.Repeat(strings.Repeat("あ", 40)+"、"+strings.Repeat("い", 8)+" ", 30) + "終わり", "、"},
		{"Chinese commas", strings.Repeat(strings.Repeat("中", 59)+"，", 30) + "完", "，"},
		{"full-width semicolons", strings.Repeat(strings.Repeat("中", 59)+"；", 30) + "完", "；"},
		// ", " still comes first.
		{"commas before 、", strings.Repeat(strings.Repeat("あ", 40)+", "+strings.Repeat("い", 8)+"、", 30) + "終わり", ","},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, units := splitTexts(t, c.text)
			if len(units) < 2 {
				t.Fatalf("%d units", len(units))
			}
			for i, u := range units {
				if n := len([]rune(u.Text)); n > MaxUnitChars {
					t.Errorf("unit %d: %d characters", i, n)
				}
				if c.ends != "" && i < len(units)-1 && !strings.HasSuffix(u.Text, c.ends) {
					t.Errorf("unit %d ends %q", i, u.Text[len(u.Text)-8:])
				}
			}
		})
	}
	// An item's number stays on its first piece: no cut at the space after it.
	for _, text := range []string{"3. " + strings.Repeat("あ", 590) + "`a、b`" + strings.Repeat("い", 100) + "。", "1. " + strings.Repeat("가", 650) + " 끝입니다."} {
		if _, units := splitTexts(t, text); len(units) < 2 || !strings.HasPrefix(units[0].Text, text[:3]) {
			t.Errorf("%q…: first unit %q", text[:6], units[0].Text[:6])
		}
	}
}

// Only the first MaxAnswerBytes are read, ended at a line break near them,
// else at a space: a long last line is read up to the limit.
func TestSplitReadsOnly64KiB(t *testing.T) {
	text := strings.Repeat("앞의 문장입니다.\n", MaxAnswerBytes/24+10) + "뒤의 문장."
	units, _ := Split("x", text, 0)
	splitCheck(t, "x", text, units, 0)
	limit := UTF16Offset(text, MaxAnswerBytes)
	for _, u := range units {
		if u.End > limit || u.Text == "뒤의 문장." {
			t.Fatalf("unit %s at %d..%d past %d", u.ID, u.Start, u.End, limit)
		}
	}

	long := "제목\n" + strings.Repeat("가 ", 70000/len("가 "))
	units, _ = Split("x", long, 0)
	splitCheck(t, "x", long, units, 0)
	if n := len(units); n < 50 || units[n-1].End > UTF16Offset(long, MaxAnswerBytes) || units[n-1].End < UTF16Offset(long, MaxAnswerBytes-splitCutNear) {
		t.Fatalf("a long last line: %d units", n)
	}
}

// splitHostile are answers made to be slow or to break a splitter: deep
// nesting, thousands of marks, brackets and quotes never closed.
func splitHostile() map[string]string {
	fill := func(unit string) string { return strings.Repeat(unit, MaxAnswerBytes/len(unit)+1) }
	var nested strings.Builder
	for i := 0; nested.Len() < MaxAnswerBytes; i++ {
		nested.WriteString(strings.Repeat(" ", i%300) + "- x. y\n")
	}
	var boldDeep strings.Builder
	for i := 0; boldDeep.Len() < MaxAnswerBytes; i++ {
		pad := strings.Repeat(" ", 2*(i%150))
		boldDeep.WriteString(pad + "**1. a**\n" + pad + "- b\n")
	}
	footnotes := (MaxAnswerBytes - 8) / 3 // "[^" and "]" each
	return map[string]string{
		"wide table":       "|" + strings.Repeat("a|", 8192) + "\n|" + strings.Repeat("-|", 8192) + "\n" + fill("|가|\n"),
		"nested footnote":  "가 " + strings.Repeat("[^", footnotes) + " " + strings.Repeat("]", footnotes),
		"asterisks":        fill("*"),
		"emphasis":         fill("*a _b **c ~~d "),
		"brackets":         fill("(\"[「'a. "),
		"dollars":          fill("$a. "),
		"double dollars":   fill("$$a\n"),
		"backslashes":      fill("\\(x \\[y "),
		"backticks":        fill("`` ` a. "),
		"links":            fill("[a](b "),
		"angles":           fill("<a <b x. "),
		"comments":         fill("<!-- x "),
		"entities":         fill("&amp;&#1234567;&x "),
		"addresses":        fill("http://a.b/(c "),
		"one sentence":     fill("가나다 "),
		"short sentences":  fill("a. "),
		"headings":         fill("# h\n"),
		"quotes":           fill(">") + " x",
		"quoted lists":     fill("> - "),
		"tables":           "|a|b|\n|-|-|\n" + fill("|x. y|z|\n"),
		"nested lists":     nested.String(),
		"bold lines":       fill("**a**\n"),
		"fences":           fill("```\n"),
		"CJK stops":        fill("あ。」"),
		"CJK quotes":       fill("「あ。」"),
		"CJK commas":       fill("あ、"),
		"inline math":      fill("$x$ "),
		"ordered items":    fill("1. a. b\n"),
		"quoted numbers":   fill("> 설명\n> 2. 둘째 " + strings.Repeat(">", 50) + " 3. x\n"),
		"rows past header": "|a|\n|-|\n" + fill("|x|`y \\| z`|$|w|$|\n"),
		"bold lines deep":  boldDeep.String(),
	}
}

func TestSplitHostileIsQuick(t *testing.T) {
	for name, text := range splitHostile() {
		t.Run(name, func(t *testing.T) {
			best := time.Hour
			var units []Unit
			for range 5 { // the best of five, as other packages may be testing at once
				start := time.Now()
				units, _ = Split("x", text, 0)
				best = min(best, time.Since(start))
			}
			splitCheck(t, "x", text, units, 0)
			t.Logf("%v for %d bytes, %d units", best, len(text), len(units))
			if best > 100*time.Millisecond {
				t.Errorf("%v for %d bytes", best, len(text))
			}
		})
	}
}

func FuzzSplit(f *testing.F) {
	for _, s := range []string{
		"서울입니다. 부산이에요?", "| a | b |\n|---|---|\n| 1 | 2 |", "- a\n  - b\n> c", "**x. y.** $a$ `b` [c](d)",
		"\xef\xbb\xbf\r\n# h\r\n\r\n```\ncode", "$$\nx\n$$\n\\[\na\n\\]", "<details><summary>s</summary>\n\nt</details>",
		"Sources:\n- [a](https://a)", "a &amp; b &#x1F600; \\* c", "😀 가.\n\n1. x\n   2) y", strings.Repeat("가, ", 400),
		"x \\&amp; `&amp;` y", "|0<<\n-|\n>|-0\n0|00",
		"> ```\n> code\n> ```\n> | a |\n> |---|\n> | 1 |", "- a\n\n        code\n- b", "**참고**\n- 문장입니다([a](https://a)).\n- [b](https://b)",
		"`C:\\` x \\``y`", "가 [^[^ ]] [1][2] A[0]", "설명\n2. 둘째\n10. 3.에", "[x]: words here\n<!--\na\n--> b", "2**10 ~~ a~~b",
		"1. 서울입니다. 둘째.\n> 2) x\n**3. y**\n1. [x] z", "東京です。「行こう。」と言った。好、的；是，", "Run `$HOME` \\$x **5**$x$ $$y$$ \\(a^2\\)\n\n| $a$ | b |\n|---|---|\n| $c$ | |",
	} {
		f.Add(s, 0)
	}
	f.Fuzz(func(t *testing.T, text string, max int) {
		max %= 50
		units, dropped := Split("f", text, max)
		splitCheck(t, "f", text, units, max)
		if dropped != 0 {
			t.Errorf("%d dropped", dropped)
		}
	})
}

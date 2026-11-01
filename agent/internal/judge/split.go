package judge

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Splitting (spec part 2). An answer is read line by line, once, and never
// rewritten: "\r\n" ends a line where it stands, a BOM is skipped by its
// offset, and every offset is into the text as given. Blocks are known by
// how their first line starts (fences, formulas, headings, tables, lists,
// quotes); paragraphs are gathered and cut into sentences, and a unit's
// Text is Strip of its raw span. Headings, code and formulas become context
// lines for the judge; Sources, rules, comments and definitions go.
//
// The reader follows what AIs write rather than all of CommonMark: a
// nested list item is one indented past its parent's marker, a table is a
// line with "|" over a delimiter row, a line that is only bold is a
// heading when it is short and does not end like a sentence, and one that
// starts with a list number ("**1. 강릉 (강원)**") is a list item whose
// list follows it.

const (
	splitParentChars   = 120     // of a parent item's text: a nested item's context
	splitHeaderChars   = 300     // of a table's header row: its rows' context
	splitHeadingChars  = 200     // of a heading kept as a context line
	splitCodeChars     = 300     // of a code block kept as a context line
	splitFormulaChars  = 200     // of a formula kept as a context line
	splitMaxBefore     = 6       // context lines kept before one unit, the last ones
	splitBoldChars     = 40      // characters of a line that is only bold and is a heading
	splitCutNear       = 4 << 10 // bytes before MaxAnswerBytes a line break or space may end the read
	splitCodePrefix    = "(code, not judged) "
	splitFormulaPrefix = "(formula, not judged) "
)

// Split cuts one answer into units, in reading order: table body rows
// (UnitRow), list items (UnitItem), block quotes (UnitQuote) and
// paragraphs (UnitSentence), the last three cut into sentences. Ids are
// "<answerID>:<n>" with n from 1. Units after the first maxUnits
// (MaxUnits, the most, when maxUnits <= 0) have Judged false; the agent
// passes agent.json judge.maxUnits or DefaultMaxUnits.
//
// It reads the first MaxAnswerBytes of text in one pass and never rewrites
// it: "\r\n" is a line break where it stands and a leading BOM is skipped,
// so Start and End are UTF-16 offsets into text exactly as given, and Text
// is Strip(Kind, raw) of that span. Math holds where Text has math the
// answer drew as math: the inline "$…$", "$$…$$", "\(…\)" and "\[…\]" the
// splitter read as math in the raw span, not code or an escaped "\$".
//
// An item's span starts after a bullet ("-", "*", "+") and its task box,
// but at the number of an ordered item ("1. 서울", "2) 부산"), so its first
// unit keeps the number, task box and all ("1. [x] 완료"): the number is
// the item's place in its list. The same holds in a quote ("> 1. x").
//
// Not units: code blocks, "$$…$$" and "\[…\]" blocks and headings (ATX,
// setext, and a line that is only bold, at most 40 characters) become
// context lines, in the Before of the next unit (after the last unit,
// SplitAnswer gives them). A line that is only bold and starts with a list
// number ("**1. 강릉 (강원)**", any length) is no heading but a list item,
// "1. 강릉 (강원)" from the number to the closing marks, and the list right
// after it is its children, at any depth. Thematic breaks, HTML comments,
// and link and footnote definitions are left out. So is a Sources block:
// a line that reads Sources, References, 출처 or 참고 once "**", "#", a
// section number ("**5. 출처**", "### 6. 참고 자료") and a closing colon
// are taken off, with the link lines after it up to a line without a
// link; a link line that reads as a sentence of its own is a unit and the
// block goes on. A unit that is only links or addresses (and a list
// number) is left out too. A row's Context is the first 300 characters of
// its table header, cells joined by " | "; a nested item's Context is the
// first 120 characters of its parent's Text, number and all (or of the
// bold numbered line its list follows).
//
// A sentence ends after . ! ? 。 ！ ？ or … when a space or the end
// follows, but not inside numbers (3.75, 1.99.1), after abbreviations
// (e.g., U.S., Dr.), after the list number an item starts with, or inside
// brackets, quotes, inline code, inline math or links. 。！？ end one with
// nothing after them too, as Japanese and Chinese write, unless they close
// a quote the words go on after ("「そうだ。」と言った。"). Closing brackets,
// quotes and marks right after an end belong to the sentence. A unit
// longer than MaxUnitChars characters is cut at its last "; " or ", "
// before the limit, else after its last 、，or ；, else at its last space.
//
// A unit whose Text is not Strip of its raw span, or not found in the
// answer, is left out and counted in dropped: a unit the phone shows is
// always the AI's own words.
func Split(answerID, text string, maxUnits int) (units []Unit, dropped int) {
	units, _, dropped = SplitAnswer(answerID, text, maxUnits)
	return units, dropped
}

// SplitAnswer is Split, and also gives the context lines after the last
// unit, which no unit's Before holds: code, a formula or a heading that
// ends the answer, or those of an answer that is only code. They go in
// Answer.Tail, so a judge sees them after the answer's last unit. Its
// units are Split's: ordered items with their numbers, and Math on each
// unit whose Text has math.
func SplitAnswer(answerID, text string, maxUnits int) (units []Unit, tail []string, dropped int) {
	if maxUnits <= 0 {
		maxUnits = MaxUnits
	}
	s := &splitter{text: text, limit: splitLimit(text), id: answerID, maxUnits: maxUnits, cursor: splitCursor{s: text}}
	pos := 0
	if strings.HasPrefix(text, "\xef\xbb\xbf") {
		pos = len("\xef\xbb\xbf")
	}
	for pos < s.limit {
		end, next := splitLineEnd(text, pos, s.limit)
		s.line(pos, end, next)
		if s.skipTo > next {
			next = s.skipTo
		}
		s.skipTo = 0
		pos = next
	}
	s.finish()
	return s.units, s.before, s.dropped
}

// splitter is one Split call.
type splitter struct {
	text     string
	limit    int // bytes read: the first MaxAnswerBytes (splitLimit)
	id       string
	maxUnits int
	cursor   splitCursor
	units    []Unit
	dropped  int
	before   []string // context lines for the next unit
	skipTo   int      // a block that ends past this line: read on from here

	para  splitPara    // the paragraph being gathered
	items []*splitItem // open list items, outermost first
	blank bool         // the line before was blank
	leads []splitLead  // bold numbered lines, outermost first: each the parent of the list after it
	bold  bool         // the line before was only bold: a paragraph in the app, which indented code does not interrupt

	table      bool   // in a table's body
	tableQuote bool   // the table is in a block quote
	header     string // its header row, cells joined by " | "
	cols       int    // its header's cells: the app drops a row's cells past them

	fence      string          // the open code fence ("```", "~~~~"), if any
	fenceQuote bool            // the fence is in a block quote
	indented   bool            // in an indented code block
	codeCol    int             // the column its lines start at
	code       strings.Builder // the first splitCodeChars characters of the open code block
	codeChars  int

	comment  bool // in an HTML comment
	footnote bool // in a footnote definition
	sources  bool // in a Sources block
}

// splitPara is a paragraph being gathered: its raw span runs from the first
// character of its first line to the last of its last line, so it holds
// the line breaks and markers between.
type splitPara struct {
	kind       string // UnitSentence, UnitItem or UnitQuote; "" when none is open
	start, end int
	context    string
	item       *splitItem // the item this is the first paragraph of, if any
	listed     bool       // a quote paragraph a list marker opened: the next marker starts a unit
}

// splitItem is an open list item.
type splitItem struct {
	marker  int    // column of its marker
	content int    // column its text starts at
	context string // its parent's text, for its units
	text    string // its first paragraph, for its children's context
	started bool   // its first paragraph has been read
}

// splitLimit is how much of text is read: the first MaxAnswerBytes, ended
// at the last line break before them when it is near (splitCutNear), else
// at the last space, else at a character boundary. A long last line is
// read up to the limit, not dropped from an early line break on.
func splitLimit(text string) int {
	if len(text) <= MaxAnswerBytes {
		return len(text)
	}
	cut := MaxAnswerBytes
	for i := 0; i < utf8.UTFMax && cut > 0 && !utf8.RuneStart(text[cut]); i++ {
		cut--
	}
	near := max(cut-splitCutNear, 0)
	for _, marks := range []string{"\r\n", " \t"} {
		if i := strings.LastIndexAny(text[near:cut], marks); i >= 0 && near+i > 0 {
			return near + i
		}
	}
	return cut
}

// splitLineEnd gives the end of the line at pos (before its "\n", "\r\n"
// or "\r") and where the next line starts.
func splitLineEnd(text string, pos, limit int) (end, next int) {
	i := strings.IndexAny(text[pos:limit], "\r\n")
	if i < 0 {
		return limit, limit
	}
	end = pos + i
	if text[end] == '\r' && end+1 < limit && text[end+1] == '\n' {
		return end, end + 2
	}
	return end, end + 1
}

// splitIndent is the indentation of the line [ls, le) in columns (a tab
// goes to the next multiple of 4) and where its text starts.
func splitIndent(t string, ls, le int) (cols, c int) {
	for c = ls; c < le; c++ {
		switch t[c] {
		case ' ':
			cols++
		case '\t':
			cols += 4 - cols%4
		default:
			return cols, c
		}
	}
	return cols, c
}

// splitTrimEnd is b moved back over spaces and tabs, not before a.
func splitTrimEnd(t string, a, b int) int {
	for b > a && (t[b-1] == ' ' || t[b-1] == '\t') {
		b--
	}
	return b
}

func (s *splitter) line(ls, le, next int) {
	t := s.text
	if s.fence != "" {
		l, in := t[ls:le], true
		if s.fenceQuote { // code in a quote: its lines start with ">"
			_, c := splitIndent(t, ls, le)
			if in = c < le && t[c] == '>'; in {
				l = t[splitUnquote(t, c, le):le]
			}
		}
		if in {
			if splitFenceCloses(l, s.fence) {
				s.endCode()
			} else {
				s.addCode(l)
			}
			return
		}
		s.endCode() // a line out of the quote ends the quote and its code
	}
	if s.comment {
		k := strings.Index(t[ls:le], "-->")
		if k < 0 {
			return
		}
		s.comment = false
		if rest := ls + k + len("-->"); strings.TrimSpace(t[rest:le]) != "" {
			s.line(rest, le, next) // the app shows what follows the comment
		}
		return
	}
	indent, c := splitIndent(t, ls, le)
	blank := c == le
	if s.indented && (blank || indent < s.codeCol) {
		s.endCode()
	}
	if s.footnote {
		if !blank && !s.blank && indent >= 2 {
			return // the definition goes on
		}
		s.footnote = false
	}
	// A Sources block goes on over blank lines and link lines. A link line
	// that reads as a sentence with a citation is a unit; a source line is
	// dropped and ends what such a sentence line before it opened, so no
	// unit takes it in.
	if s.sources && !blank {
		if !splitHasLink(t[c:le]) {
			s.sources = false
		} else if !splitSentenceLine(t, c, le) {
			s.flushPara()
			return
		}
	}
	if blank {
		s.flushPara()
		s.table = false
		s.blank = true
		s.bold = false
		return
	}
	s.block(indent, c, le, next)
	s.blank = false
}

// block reads a line that is not blank and not inside code or a comment.
func (s *splitter) block(indent, c, le, next int) {
	t := s.text
	end := splitTrimEnd(t, c, le)
	l := t[c:end]
	afterBold := s.bold
	s.bold = false
	// Indented code: four columns past the text of the list item it is in,
	// where no paragraph goes on. It comes first: "    - name: x" is code.
	if col := s.itemCol(indent) + 4; s.indented || indent >= col && s.para.kind == "" && !s.table && !afterBold {
		if !s.indented {
			s.closeItems(indent)
			s.indented, s.codeCol = true, col
		}
		s.addCode(l)
		return
	}
	if l[0] == '>' {
		s.endLeads(s.itemDepth(indent))
		s.quote(c, end, next)
		return
	}
	if f := splitFence(l); f != "" {
		s.flushPara()
		s.table = false
		s.fence = f
		return
	}
	if stop, inner, ok := splitBlockMath(t, c, s.limit); ok {
		s.flushPara()
		s.table = false
		s.context(splitFormulaPrefix + splitClip(inner, splitFormulaChars))
		s.skipTo = stop
		return
	}
	if strings.HasPrefix(l, "<!--") {
		i := strings.Index(l[4:], "-->")
		if i < 0 {
			s.flushPara()
			s.comment = true
			return
		}
		if strings.TrimSpace(l[4+i+3:]) == "" {
			s.flushPara()
			return
		}
	}
	if s.table {
		if !s.tableQuote && strings.IndexByte(l, '|') >= 0 {
			s.unit(UnitRow, c, splitRowEnd(t, c, end, s.cols), s.header)
			return
		}
		s.table = false // a line out of a quote ends the quote's table
	}
	if strings.IndexByte(l, '|') >= 0 && next < s.limit {
		if nl, nn := splitLineEnd(t, next, s.limit); splitDelimiterRow(t[next:nl]) {
			s.flushPara()
			s.table, s.tableQuote, s.header, s.cols = true, false, splitClip(Strip(UnitRow, l), splitHeaderChars), len(splitCells(l))
			s.skipTo = nn
			s.endLeads(s.itemDepth(indent))
			return
		}
	}
	if splitATX(l) {
		s.flushPara()
		s.closeItems(indent)
		s.heading(l)
		return
	}
	if indent <= 3 {
		if splitUnderline(l) && s.para.kind == UnitSentence {
			p := s.para
			s.para = splitPara{}
			s.heading(strings.TrimSpace(t[p.start:p.end]))
			return
		}
		if splitThematic(l) {
			s.flushPara()
			s.closeItems(indent)
			s.endLeads(len(s.items))
			return
		}
	}
	if l[0] == '[' {
		if label, ok := splitDefinition(l); ok {
			s.flushPara()
			s.footnote = strings.HasPrefix(label, "^")
			return
		}
	}
	if l[0] == '<' {
		if only, summary := splitTagLine(l); only {
			s.flushPara()
			s.table = false
			if summary != "" {
				s.context(splitClip(summary, splitHeadingChars))
			}
			return
		}
	}
	if splitSourcesLabel(l) || splitSourcesLine(l) {
		s.flushPara()
		s.sources = true
		s.leads = nil
		return
	}
	if m, cs := splitListMarker(t, c, end); m > 0 && (s.para.kind != UnitSentence || splitInterrupts(t, c, m, cs, end)) {
		s.listItem(indent, c, m, cs, end)
		return
	}
	if a, b, ok := splitBoldItem(l); ok && !s.underlined(next) {
		s.boldItem(indent, c+a, c+b)
		return
	} else if ok { // "**1. 강릉**" over "---" is a heading in the app
		s.flushPara()
		s.closeItems(indent)
		s.heading(l)
		return
	}
	if splitBoldHeading(l) {
		s.flushPara()
		s.closeItems(indent)
		s.heading(l)
		s.bold = true
		return
	}
	s.textLine(indent, c, end)
}

// itemCol is the column the text of the innermost open list item that a
// line at indent is in starts at, or 0 outside lists.
func (s *splitter) itemCol(indent int) int {
	for n := len(s.items); n > 0; n-- {
		if s.items[n-1].content <= indent {
			return s.items[n-1].content
		}
	}
	return 0
}

// heading keeps a heading as a context line, unless it opens a Sources
// block.
func (s *splitter) heading(l string) {
	s.endLeads(len(s.items)) // not one inside the list under a bold numbered line
	if splitSourcesLabel(l) {
		s.sources = true
		return
	}
	s.context(splitClip(l, splitHeadingChars))
}

// textLine is a line of a paragraph: it goes on the open paragraph unless
// a blank line came between, and starts one otherwise.
func (s *splitter) textLine(indent, c, end int) {
	if s.para.kind != "" && !s.blank {
		s.para.end = end
		return
	}
	s.flushPara()
	s.closeItems(indent)
	if n := len(s.items); n > 0 {
		it := s.items[n-1]
		p := splitPara{kind: UnitItem, start: c, end: end, context: it.context}
		if !it.started {
			p.item = it
		}
		s.para = p
		s.endLeads(n)
		return
	}
	s.para = splitPara{kind: UnitSentence, start: c, end: end}
	s.leads = nil
}

// listItem opens a list item whose marker is at c and text at cs. Its
// parent is the open item it is nested in, or the bold numbered line it
// follows at that depth, if any. The span of a bullet's first paragraph
// starts at cs, after the marker and task box; an ordered item's starts at
// its number (splitItemStart), so its first unit shows "1. 서울".
func (s *splitter) listItem(indent, c, m, cs, end int) {
	s.flushPara()
	s.table = false
	for n := len(s.items); n > 0 && indent <= s.items[n-1].marker; n-- {
		s.items = s.items[:n-1]
	}
	for n := len(s.leads); n > 0 && s.leads[n-1].n > len(s.items); n-- {
		s.leads = s.leads[:n-1] // the item the bold line was in has ended
	}
	ctx := ""
	if n := len(s.items); n > 0 {
		ctx = s.items[n-1].text
	}
	if n := len(s.leads); n > 0 && s.leads[n-1].n == len(s.items) {
		ctx = s.leads[n-1].text
	}
	col := indent + m - c + 1
	if sp := cs - m; sp >= 1 && sp <= 4 && cs < end {
		col = indent + m - c + sp
	}
	it := &splitItem{marker: indent, content: col, context: ctx}
	s.items = append(s.items, it)
	if cs < end {
		s.para = splitPara{kind: UnitItem, start: splitItemStart(s.text, c, cs), end: end, context: ctx, item: it}
	}
}

// splitItemStart is where the span of a list item whose marker is at c and
// text at cs starts: at the number of an ordered marker ("1.", "2)"), which
// stays in its Text, and after a bullet ("-", "*", "+") and its task box.
func splitItemStart(t string, c, cs int) int {
	if mathDigit(t[c]) {
		return c
	}
	return cs
}

// splitNumberText is where the words of a span that starts at i with a
// list number ("1. ", "2) ", up to three digits) start: past the number,
// its spaces and a task box ("1. [x] 완료"). It is i when the span starts
// with no list number. A sentence is read from there, so no period of the
// number ends it, and the number is no content of its own.
func splitNumberText(src string, i, to int) int {
	if i >= to || !mathDigit(src[i]) {
		return i
	}
	if m, cs := splitListMarker(src, i, to); m > 0 {
		return cs
	}
	return i
}

// boldItem reads a line that is only bold and starts with a list number
// (splitBoldItem), its text from the number at [a, b): a list item whose
// units end with the line ("1. 강릉 (강원)"), as AIs write the items of a
// list with the details under each ("**1. 강릉 (강원)**" over "- 이동: …").
// It is the parent of the list right after it, at the depth of the open
// items it is in, until a paragraph, heading, rule, table, quote or
// Sources block at that depth comes between.
func (s *splitter) boldItem(indent, a, b int) {
	s.flushPara()
	s.table = false
	s.closeItems(indent)
	ctx := ""
	if n := len(s.items); n > 0 {
		ctx = s.items[n-1].text
	}
	s.para = splitPara{kind: UnitItem, start: a, end: b, context: ctx}
	s.flushPara()
	s.endLeads(len(s.items))
	s.leads = append(s.leads, splitLead{splitClip(Strip(UnitItem, s.text[a:b]), splitParentChars), len(s.items)})
	s.bold = true
}

// underlined: the line at next is a setext underline ("---", "==="), which
// makes the line before it a heading in the app.
func (s *splitter) underlined(next int) bool {
	if next >= s.limit {
		return false
	}
	nl, _ := splitLineEnd(s.text, next, s.limit)
	ni, nc := splitIndent(s.text, next, nl)
	u := s.text[nc:splitTrimEnd(s.text, nc, nl)]
	return ni <= 3 && u != "" && splitUnderline(u)
}

// splitLead is a bold numbered line: its text and the open items it is in.
type splitLead struct {
	text string
	n    int
}

// endLeads ends the bold numbered lines in depth or more open items: a
// line at that depth came between them and their list.
func (s *splitter) endLeads(depth int) {
	for n := len(s.leads); n > 0 && s.leads[n-1].n >= depth; n-- {
		s.leads = s.leads[:n-1]
	}
}

// itemDepth is how many open list items a line at indent is in.
func (s *splitter) itemDepth(indent int) int {
	for n := len(s.items); n > 0; n-- {
		if s.items[n-1].content <= indent {
			return n
		}
	}
	return 0
}

// closeItems closes the list items a line at indent is not inside.
func (s *splitter) closeItems(indent int) {
	for n := len(s.items); n > 0 && s.items[n-1].content > indent; n-- {
		s.items = s.items[:n-1]
	}
}

// quote reads a block quote line whose first ">" is at c. It ends a table
// outside the quote, as in GFM, so a "|" line after it goes on the quote
// and units stay in reading order. Code fences and tables inside the quote
// are read as they are outside it, on the text after the ">" marks. A list
// item or a bold numbered line in a quote is a quote unit of its own,
// which keeps its number as an item does ("> 1. x" shows "1. x").
func (s *splitter) quote(c, end, next int) {
	if !s.tableQuote {
		s.table = false
	}
	t := s.text
	q := splitUnquote(t, c, end)
	if q == end { // an empty quote line ends the quote's paragraph and table
		if s.para.kind == UnitQuote {
			s.flushPara()
		}
		s.table = false
		return
	}
	l := t[q:end]
	if f := splitFence(l); f != "" {
		s.flushPara()
		s.table = false
		s.fence, s.fenceQuote = f, true
		return
	}
	if s.table {
		if strings.IndexByte(l, '|') >= 0 {
			s.unit(UnitRow, q, splitRowEnd(t, q, end, s.cols), s.header)
			return
		}
		s.table = false
	}
	if strings.IndexByte(l, '|') >= 0 && next < s.limit {
		nl, nn := splitLineEnd(t, next, s.limit)
		if _, nc := splitIndent(t, next, nl); nc < nl && t[nc] == '>' && splitDelimiterRow(t[splitUnquote(t, nc, nl):nl]) {
			s.flushPara()
			s.table, s.tableQuote, s.header, s.cols = true, true, splitClip(Strip(UnitRow, l), splitHeaderChars), len(splitCells(l))
			s.skipTo = nn
			return
		}
	}
	if a, b, ok := splitBoldItem(l); ok { // a unit of its own, as out of a quote
		s.flushPara()
		s.para = splitPara{kind: UnitQuote, start: q + a, end: q + b}
		s.flushPara()
		return
	}
	if splitATX(l) || splitBoldHeading(l) {
		s.flushPara()
		s.heading(l)
		return
	}
	// An ordered one keeps its number, as out of a quote; inside a quote's
	// paragraph only what may interrupt one starts an item ("> 출시일은\n>
	// 10. 3.에 공개" is one sentence, as out of a quote).
	if m, cs := splitListMarker(t, q, end); m > 0 && (s.para.kind != UnitQuote || s.para.listed || splitInterrupts(t, q, m, cs, end)) {
		s.flushPara()
		if cs < end {
			s.para = splitPara{kind: UnitQuote, start: splitItemStart(t, q, cs), end: end, listed: true}
		}
		return
	}
	if s.para.kind == UnitQuote && !s.blank {
		s.para.end = end
		return
	}
	s.flushPara()
	s.para = splitPara{kind: UnitQuote, start: q, end: end}
}

// splitUnquote is where the text of a quote line starts: past its ">"
// marks, the first at c, and the spaces after each.
func splitUnquote(t string, c, end int) int {
	q := c
	for q < end && t[q] == '>' {
		q++
		for q < end && (t[q] == ' ' || t[q] == '\t') {
			q++
		}
	}
	return q
}

func (s *splitter) finish() {
	if s.fence != "" || s.indented {
		s.endCode()
	}
	s.flushPara()
}

// context keeps a context line for the next unit.
func (s *splitter) context(line string) {
	s.before = append(s.before, line)
	if len(s.before) > splitMaxBefore {
		s.before = s.before[len(s.before)-splitMaxBefore:]
	}
}

func (s *splitter) addCode(line string) {
	for _, r := range line {
		if s.codeChars >= splitCodeChars {
			return
		}
		s.code.WriteRune(r)
		s.codeChars++
	}
	if s.codeChars < splitCodeChars {
		s.code.WriteByte('\n')
		s.codeChars++
	}
}

func (s *splitter) endCode() {
	code := strings.TrimSpace(s.code.String())
	s.fence, s.fenceQuote, s.indented = "", false, false
	s.code.Reset()
	s.codeChars = 0
	if code != "" {
		s.context(splitCodePrefix + code)
	}
}

// Line shapes.

// splitFence is the fence a line opens ("```", "~~~~"), or "".
func splitFence(l string) string {
	if len(l) < 3 || (l[0] != '`' && l[0] != '~') {
		return ""
	}
	n := 0
	for n < len(l) && l[n] == l[0] {
		n++
	}
	if n < 3 || (l[0] == '`' && strings.IndexByte(l[n:], '`') >= 0) {
		return ""
	}
	return l[:n]
}

// splitFenceCloses: the line closes the fence: at least as many of its
// marks and nothing after them.
func splitFenceCloses(line, fence string) bool {
	l := strings.TrimSpace(line)
	n := 0
	for n < len(l) && l[n] == fence[0] {
		n++
	}
	return n >= len(fence) && n == len(l)
}

// splitATX: "#" to "######" and then a space or the end.
func splitATX(l string) bool {
	n := 0
	for n < len(l) && l[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && (n == len(l) || l[n] == ' ' || l[n] == '\t')
}

// splitUnderline: a setext heading's underline, all "=" or all "-".
func splitUnderline(l string) bool {
	return strings.Trim(l, "=") == "" || strings.Trim(l, "-") == ""
}

// splitThematic: three or more of one of "-", "*" or "_", spaces between.
func splitThematic(l string) bool {
	n := 0
	for i := 0; i < len(l); i++ {
		switch {
		case l[i] == l[0] && (l[0] == '-' || l[0] == '*' || l[0] == '_'):
			n++
		case l[i] == ' ' || l[i] == '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// splitDelimiterRow: a table's delimiter row, like "|---|:---:|".
func splitDelimiterRow(line string) bool {
	l := strings.TrimSpace(line)
	if strings.IndexByte(l, '|') < 0 {
		return false
	}
	l = strings.TrimSuffix(strings.TrimPrefix(l, "|"), "|")
	for _, cell := range strings.Split(l, "|") {
		cell = strings.TrimSpace(cell)
		cell = strings.TrimSuffix(strings.TrimPrefix(cell, ":"), ":")
		if cell == "" || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

// splitDefinition: a link definition "[x]: url" or a footnote definition
// "[^x]: …"; label is what is between the brackets.
func splitDefinition(l string) (label string, ok bool) {
	j := strings.Index(l, "]:")
	if j < 2 {
		return "", false
	}
	label = l[1:j]
	if strings.ContainsAny(label, "[]") {
		return "", false
	}
	if label[0] == '^' {
		return label, !strings.ContainsAny(label, " \t")
	}
	return label, splitDestination(strings.TrimSpace(l[j+2:]))
}

// splitDestination: what follows "[x]:" is a link definition's, as in
// CommonMark: one run with no space or "<…>", then nothing or a title in
// quotes or brackets. "[Note]: The deadline moved." is a sentence.
func splitDestination(rest string) bool {
	if rest == "" {
		return false
	}
	k := strings.IndexAny(rest, " \t")
	if rest[0] == '<' {
		k = strings.IndexByte(rest, '>') + 1
	}
	if k <= 0 {
		return k < 0
	}
	title := strings.TrimSpace(rest[k:])
	if title == "" {
		return true
	}
	open, close := title[0], title[len(title)-1]
	return len(title) >= 2 && (open == '"' && close == '"' || open == '\'' && close == '\'' || open == '(' && close == ')')
}

// splitTagLine: the line is only HTML tags, as "<details>" or "</div>"
// are; summary is the words of a "<summary>" line, kept as a heading.
func splitTagLine(l string) (only bool, summary string) {
	var rest strings.Builder
	gt := -1
	for i := 0; i < len(l); i++ {
		if l[i] == '<' && i+1 < len(l) && (mathLetter(l[i+1]) || l[i+1] == '/' || l[i+1] == '!') {
			if gt < i {
				gt = strings.IndexByte(l[i:], '>')
				if gt < 0 {
					rest.WriteString(l[i:])
					break
				}
				gt += i
			}
			i = gt
			continue
		}
		rest.WriteByte(l[i])
	}
	words := strings.TrimSpace(rest.String())
	if strings.Contains(strings.ToLower(l), "<summary") {
		return true, words
	}
	return words == "", ""
}

var splitBoldMarks = strings.NewReplacer("**", "", "__", "")

// splitSourcesLabel: the line names a Sources block once "**", "__", "#"
// and a closing colon are taken off.
func splitSourcesLabel(l string) bool {
	if len(l) > 64 {
		return false
	}
	s := splitBoldMarks.Replace(l)
	s = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "#"))
	if s != "" && mathDigit(s[0]) && strings.ContainsRune("*_#", rune(l[0])) { // a numbered section: "**5. 출처**", "### 6. 참고 자료"
		if m, cs := splitListMarker(s, 0, len(s)); m > 0 {
			s = s[cs:]
		}
	}
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(s, ":"), "："))
	if len(s) > 32 {
		return false
	}
	switch strings.ToLower(s) {
	case "sources", "source", "references", "reference", "출처", "참고", "참고 자료", "참고자료":
		return true
	}
	return false
}

// splitSourcesLine: a Sources label and then only links, on one line
// ("Sources: [a](…), [b](…)").
func splitSourcesLine(l string) bool {
	i := strings.IndexAny(l, ":：")
	if i < 0 || !splitSourcesLabel(l[:i]) {
		return false
	}
	_, size := utf8.DecodeRuneInString(l[i:])
	rest := strings.TrimLeft(l[i+size:], "*_ \t")
	if rest == "" {
		return false
	}
	_, content, _ := splitStrip(UnitSentence, rest)
	return !content
}

// splitHasLink: the line holds a link or a web address.
func splitHasLink(l string) bool {
	return strings.Contains(l, "](") || strings.Contains(l, "http://") || strings.Contains(l, "https://") || strings.Contains(l, "www.")
}

// splitSentenceLine: the line [c, le) of a Sources block, past a list
// marker, reads as a sentence and not as a source: it has words of its own
// outside links and addresses, and it ends like a sentence ("월 환산액은
// …원입니다([고용노동부](…)).", closers allowed), not right after a digit,
// as a source's date does ("(2025.8.5.)", ", 2024."). Such a line is a
// unit; "- [Title](url)" or "통계청, 2025 (https://…)" is not.
func splitSentenceLine(t string, c, le int) bool {
	end := splitTrimEnd(t, c, le)
	if m, cs := splitListMarker(t, c, end); m > 0 {
		c = cs
	}
	text, content, _ := splitStrip(UnitSentence, t[c:end])
	body := strings.TrimRight(text, splitClosers)
	r, size := utf8.DecodeLastRuneInString(body)
	b, _ := utf8.DecodeLastRuneInString(body[:len(body)-size])
	return content && splitTerminator(r) && (b < '0' || b > '9')
}

// splitListMarker reads a list marker at c ("-", "*", "+", "1." or "1)",
// and a space, or the end of the line): m is the offset after the marker
// and cs where the item's text starts, past a task box ("[ ]", "[x]").
// m is 0 when there is no marker. A number has at most three digits, so
// "2026. 10. 30. 기준" starts no list.
func splitListMarker(t string, c, end int) (m, cs int) {
	i := c
	switch t[i] {
	case '-', '*', '+':
		i++
	default:
		j := i
		for j < end && j-i < 3 && mathDigit(t[j]) {
			j++
		}
		if j == i || j >= end || (t[j] != '.' && t[j] != ')') {
			return 0, 0
		}
		i = j + 1
	}
	if i < end && t[i] != ' ' && t[i] != '\t' {
		return 0, 0
	}
	cs = i
	for cs < end && (t[cs] == ' ' || t[cs] == '\t') {
		cs++
	}
	if cs+3 <= end && t[cs] == '[' && strings.IndexByte(" xX", t[cs+1]) >= 0 && t[cs+2] == ']' && (cs+3 == end || t[cs+3] == ' ' || t[cs+3] == '\t') {
		for cs += 3; cs < end && (t[cs] == ' ' || t[cs] == '\t'); cs++ {
		}
	}
	return i, cs
}

// splitInterrupts: the list item whose marker is at c may start inside an
// open paragraph, as in CommonMark and the app: it is not empty, and an
// ordered one starts at 1. So "출시일은\n10. 3.에 공개" stays one sentence.
func splitInterrupts(t string, c, m, cs, end int) bool {
	if cs >= end {
		return false
	}
	if t[c] == '-' || t[c] == '*' || t[c] == '+' {
		return true
	}
	n, err := strconv.Atoi(t[c : m-1])
	return err == nil && n == 1
}

// splitBoldHeading: the line is only bold ("**…**" or "__…__", maybe with
// a colon after), at most splitBoldChars characters, does not end like a
// sentence and does not start with a list number (splitBoldItem).
func splitBoldHeading(l string) bool {
	if _, _, ok := splitBoldItem(l); ok {
		return false
	}
	for _, mark := range []string{"**", "__"} {
		body := strings.TrimSuffix(strings.TrimSuffix(l, ":"), "：")
		if !strings.HasPrefix(body, mark) || !strings.HasSuffix(body, mark) || len(body) <= 2*len(mark) {
			continue
		}
		inner := body[len(mark) : len(body)-len(mark)]
		if strings.Contains(inner, mark) || strings.TrimSpace(inner) != inner {
			continue
		}
		text := Strip(UnitSentence, inner)
		if text == "" || utf8.RuneCountInString(text) > splitBoldChars {
			return false
		}
		r, _ := utf8.DecodeLastRuneInString(text)
		return !splitTerminator(r)
	}
	return false
}

// splitBoldItem: the line is only bold, as splitBoldHeading reads it, and
// its bold text starts with a list number ("**1. 강릉 (강원)**", "__2)
// 전주__"); [a, b) is its text from the number up to the closing marks
// ("1. 강릉 (강원)"). The number has at most three digits, as
// splitListMarker reads it, text follows it, and a date's month ("**10.
// 30. 기준**") is none.
func splitBoldItem(l string) (a, b int, ok bool) {
	body := strings.TrimSuffix(strings.TrimSuffix(l, ":"), "：")
	for _, mark := range []string{"**", "__"} {
		if !strings.HasPrefix(body, mark) || !strings.HasSuffix(body, mark) || len(body) <= 2*len(mark) {
			continue
		}
		start, end := len(mark), len(body)-len(mark)
		inner := body[start:end]
		if strings.Contains(inner, mark) || strings.TrimSpace(inner) != inner || !mathDigit(inner[0]) {
			return 0, 0, false
		}
		m, cs := splitListMarker(body, start, end)
		if m == 0 || cs >= end || splitDayNumber(body[cs:end]) {
			return 0, 0, false
		}
		return start, end, true
	}
	return 0, 0, false
}

// splitDayNumber: s starts with the day of a dotted date ("30. 기준"), one
// or two digits and a period, then a space or the end.
func splitDayNumber(s string) bool {
	d := 0
	for d < len(s) && d < 3 && mathDigit(s[d]) {
		d++
	}
	return d > 0 && d <= 2 && d < len(s) && s[d] == '.' && (d+1 == len(s) || s[d+1] == ' ' || s[d+1] == '(')
}

// splitBlockMath reads a formula on lines of its own at t[c] ("$$…$$" or
// "\[…\]" as the app's blockMathAt does): the offset after its last line,
// its TeX, and whether it is one.
func splitBlockMath(t string, c, limit int) (stop int, inner string, ok bool) {
	var close int
	switch {
	case strings.HasPrefix(t[c:limit], "$$"):
		i := strings.Index(t[c+2:limit], "$$")
		if i < 0 {
			return 0, "", false
		}
		close = c + 2 + i
		inner = t[c+2 : close]
		close += 2
	case strings.HasPrefix(t[c:limit], `\[`):
		for i := c + 2; i+1 < limit && close == 0; i++ {
			if t[i] != '\\' {
				continue
			}
			switch t[i+1] {
			case '\\':
				i++
			case '[':
				return 0, "", false
			case ']':
				close = i + 2
				inner = t[c+2 : i]
			}
		}
		if close == 0 {
			return 0, "", false
		}
	default:
		return 0, "", false
	}
	if splitBlankLine(inner) {
		return 0, "", false
	}
	rest := close
	for rest < limit && (t[rest] == ' ' || t[rest] == '\t') {
		rest++
	}
	if rest < limit && t[rest] != '\n' && t[rest] != '\r' {
		return 0, "", false
	}
	tex := mathTrim(inner)
	if tex == "" {
		return 0, "", false
	}
	if t[c] == '$' {
		bare := mathBare(tex)
		if !mathLettersOrDigits(tex) || mathKorean(bare) || mathProse(bare) {
			return 0, "", false
		}
	} else {
		// "\[" alone on its line (as Codex writes it) is always a formula
		first := strings.IndexAny(inner, "\r\n")
		alone := first >= 0 && strings.Trim(inner[:first], " \t") == ""
		if !alone && !mathHasCommand(tex) && !strings.ContainsAny(tex, "^_=+<>") {
			return 0, "", false
		}
	}
	_, stop = splitLineEnd(t, rest, limit)
	return stop, tex, true
}

// splitBlankLine: s holds a line with nothing but spaces between two line
// breaks.
func splitBlankLine(s string) bool {
	for i := strings.IndexByte(s, '\n'); i >= 0; {
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r') {
			j++
		}
		if j < len(s) && s[j] == '\n' {
			return true
		}
		k := strings.IndexByte(s[j:], '\n')
		if k < 0 {
			return false
		}
		i = j + k
	}
	return false
}

// splitClip is s, trimmed, cut to n characters.
func splitClip(s string, n int) string {
	s = strings.TrimSpace(s)
	i := 0
	for k := range s {
		if i == n {
			return s[:k]
		}
		i++
	}
	return s
}

// Sentences.

// flushPara cuts the open paragraph into units.
func (s *splitter) flushPara() {
	p := s.para
	s.para = splitPara{}
	if p.kind == "" || p.start >= p.end {
		return
	}
	if p.item != nil && !p.item.started {
		p.item.started = true
		p.item.text = splitClip(Strip(UnitItem, s.text[p.start:p.end]), splitParentChars)
	}
	for _, sp := range splitSentences(s.text, p.start, p.end, p.kind == UnitQuote) {
		s.unit(p.kind, sp[0], sp[1], p.context)
	}
}

// unit keeps the span [a, b) as one unit, or as several when it is longer
// than MaxUnitChars. A span that shows no letter or digit, or only links,
// addresses and a list number, is no unit.
func (s *splitter) unit(kind string, a, b int, context string) {
	text, content, math := splitStrip(kind, s.text[a:b])
	if !content || !splitHasLetter(text) {
		return
	}
	if utf8.RuneCountInString(text) <= MaxUnitChars {
		s.add(kind, a, b, text, math, context)
		return
	}
	for _, p := range splitCut(s.text, a, b, kind == UnitQuote) {
		if t, c, m := splitStrip(kind, s.text[p[0]:p[1]]); c && splitHasLetter(t) {
			s.add(kind, p[0], p[1], t, m, context)
		}
	}
}

// add keeps a unit, unless its text is not the AI's own words. math are
// the byte spans of the math in text (splitStrip), which Unit.Math holds
// in UTF-16 code units.
func (s *splitter) add(kind string, a, b int, text string, math []MathSpan, context string) {
	if a >= b || !splitVerbatim(text, s.text[a:b]) {
		s.dropped++
		return
	}
	var spans []UnitMath
	if len(math) > 0 {
		at := splitCursor{s: text} // spans come in order
		spans = make([]UnitMath, len(math))
		for k, m := range math {
			spans[k] = UnitMath{Start: at.at(m.Start), End: at.at(m.End), Display: m.Display}
		}
	}
	n := len(s.units) + 1
	s.units = append(s.units, Unit{
		ID:      s.id + ":" + strconv.Itoa(n),
		N:       n,
		Kind:    kind,
		Text:    text,
		Context: context,
		Start:   s.cursor.at(a),
		End:     s.cursor.at(b),
		Judged:  n <= s.maxUnits,
		Math:    spans,
		Before:  s.before,
	})
	s.before = nil
}

// splitCursor turns byte offsets into UTF-16 offsets going forward through
// the text, so Split stays linear: units come in order.
type splitCursor struct {
	s    string
	b, u int
}

func (c *splitCursor) at(b int) int {
	if b < c.b {
		c.b, c.u = 0, 0
	}
	for c.b < b {
		_, size, n := mathRune(c.s, c.b)
		if c.b+size > b {
			break
		}
		c.b += size
		c.u += n
	}
	return c.u
}

// splitVerbatim: every character text shows, but spaces and the " | "
// between cells, is in raw and in order. An entity in raw counts as the
// character it stands for. A shown "&" is matched as itself, never as a
// whole entity: Strip leaves "&amp;" as written in code and after a
// backslash, and the "&" alone is never the worse match.
func splitVerbatim(text, raw string) bool {
	j := 0
	for _, r := range text {
		if r == ' ' || r == '|' {
			continue
		}
		found := false
		for j < len(raw) && !found {
			if raw[j] == '&' && r != '&' {
				if e, show := splitEntity(raw, j, len(raw)); e > 0 {
					if er, _ := utf8.DecodeRuneInString(show); er == r {
						j, found = e, true
						continue
					}
				}
			}
			rr, size := utf8.DecodeRuneInString(raw[j:])
			j += size
			found = rr == r
		}
		if !found {
			return false
		}
	}
	return true
}

func splitHasLetter(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// splitTerminator: a character that may end a sentence.
func splitTerminator(r rune) bool {
	switch r {
	case '.', '!', '?', '。', '！', '？', '…':
		return true
	}
	return false
}

// splitAbbrev are words a period does not end a sentence after.
var splitAbbrev = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "vs": true, "mr": true, "mrs": true, "ms": true, "dr": true,
	"prof": true, "approx": true, "fig": true, "cf": true, "u.s": true, "u.k": true, "inc": true,
	"ltd": true, "jr": true, "sr": true, "jan": true, "feb": true, "mar": true, "apr": true, "jun": true,
	"jul": true, "aug": true, "sep": true, "sept": true, "oct": true, "nov": true, "dec": true,
}

// splitTitles are the abbreviations of splitAbbrev that come before a name.
var splitTitles = map[string]bool{"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true}

// splitClosers may follow a sentence's last mark and still belong to it.
const splitClosers = `)]}）］」』】〉》”’"'*_~`

// splitPairs are the brackets and quotes a sentence does not end inside;
// a straight quote opens and closes.
var splitPairs = map[rune]rune{
	'(': ')', '[': ']', '{': '}', '（': '）', '［': '］', '「': '」', '『': '』', '【': '】',
	'〈': '〉', '《': '》', '“': '”', '‘': '’', '"': '"', '\'': '\'',
}

// splitGuard is a prefix sum over a span of the text: Count(x) > 0 when x
// is inside one of the ranges added.
type splitGuard struct {
	from int
	sum  []int32
}

func splitNewGuard(from, to int) *splitGuard {
	return &splitGuard{from: from, sum: make([]int32, to-from+2)}
}

func (g *splitGuard) add(a, b int) {
	if a < b {
		g.sum[a-g.from]++
		g.sum[b-g.from]--
	}
}

func (g *splitGuard) done() {
	for i := 1; i < len(g.sum); i++ {
		g.sum[i] += g.sum[i-1]
	}
}

func (g *splitGuard) in(x int) bool { return g.sum[x-g.from] > 0 }

// splitProtect is where a sentence is never cut: code, math, links with
// their targets, addresses, entities, escapes, footnote marks and tags.
// ends maps a footnote mark or a citation to its end, as one may follow a
// sentence's last mark.
func splitProtect(toks []splitTok, from, to int) (g *splitGuard, ends map[int]int) {
	g = splitNewGuard(from, to)
	for _, t := range toks {
		switch t.kind {
		case splitTokText, splitTokDelim, splitTokSpace:
			continue
		case splitTokCite:
			if ends == nil {
				ends = map[int]int{}
			}
			ends[t.start] = t.end
		case splitTokDrop:
			if t.foot {
				if ends == nil {
					ends = map[int]int{}
				}
				ends[t.start] = t.end
			}
		}
		g.add(t.start, max(t.end, t.link))
	}
	g.done()
	return g, ends
}

// splitBrackets marks where the text is inside matched brackets and inside
// matched quotes: in(x) for x after an opening mark up to and with its
// closing one. They differ at a sentence's end: '"Stop." Then' ends after
// the quote, "그는 (정말?) 그렇다" does not end after the bracket.
func splitBrackets(src string, toks []splitTok, from, to int) (brackets, quotes *splitGuard) {
	brackets, quotes = splitNewGuard(from, to), splitNewGuard(from, to)
	type open struct {
		at    int
		mark  rune
		close rune
	}
	var stack []open
	for _, t := range toks {
		if t.kind != splitTokText {
			continue
		}
		for i := t.start; i < t.end; {
			r, size := utf8.DecodeRuneInString(src[i:])
			if n := len(stack); n > 0 && r == stack[n-1].close && splitMayClose(src, i, size, r, from, to) {
				g := brackets
				if strings.ContainsRune(splitQuoteMarks, stack[n-1].mark) {
					g = quotes
				}
				g.add(stack[n-1].at+1, i+size)
				stack = stack[:n-1]
			} else if c, ok := splitPairs[r]; ok && splitMayOpen(src, i, size, r, from, to) {
				stack = append(stack, open{i, r, c})
			}
			i += size
		}
	}
	brackets.done()
	quotes.done()
	return brackets, quotes
}

// splitQuoteMarks open quotes; the other pairs are brackets.
const splitQuoteMarks = `"'“‘「『`

// splitMayOpen and splitMayClose keep apostrophes ("don't", "Katz's") from
// being quotes: a straight single quote opens only before a word and after
// none, and closes only after a word and before none. A straight double
// quote right after a digit is an inch mark (6.1"), which opens nothing.
func splitMayOpen(src string, i, size int, r rune, from, to int) bool {
	switch r {
	case '"':
		b := splitRuneBefore(src, i, from)
		return b < '0' || b > '9'
	case '\'':
		return !splitWordRune(splitRuneBefore(src, i, from)) && !unicode.IsSpace(splitRuneAt(src, i+size, to))
	}
	return true
}

func splitMayClose(src string, i, size int, r rune, from, to int) bool {
	if r != '\'' {
		return true
	}
	return !unicode.IsSpace(splitRuneBefore(src, i, from)) && !splitWordRune(splitRuneAt(src, i+size, to))
}

func splitWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// splitRuneBefore is the character before i, or a space at from.
func splitRuneBefore(src string, i, from int) rune {
	if i <= from {
		return ' '
	}
	r, _ := utf8.DecodeLastRuneInString(src[from:i])
	return r
}

// splitRuneAt is the character at i, or a space at to.
func splitRuneAt(src string, i, to int) rune {
	if i >= to {
		return ' '
	}
	r, _ := utf8.DecodeRuneInString(src[i:to])
	return r
}

// splitSentences cuts the paragraph [from, to) into sentence spans. The
// first sentence of an ordered item starts at its number ("1. 서울입니다.")
// and is read from its words on (splitNumberText): as before the number
// was kept, no period of the number ends it, and "1. J. Smith" is still
// an initial at the start.
func splitSentences(src string, from, to int, quote bool) [][2]int {
	toks := splitInline(src, from, to, quote)
	guard, ends := splitProtect(toks, from, to)
	brackets, quotes := splitBrackets(src, toks, from, to)
	var out [][2]int
	start := splitSkipSpace(src, from, to, quote)
	words := splitNumberText(src, start, to) // where the sentence is read from
	for i := words; i < to; {
		r, size := utf8.DecodeRuneInString(src[i:to])
		if !splitTerminator(r) || guard.in(i) {
			i += size
			continue
		}
		e := i + size
		for e < to {
			r2, n := utf8.DecodeRuneInString(src[e:to])
			if !splitTerminator(r2) || guard.in(e) {
				break
			}
			if r2 == '。' || r2 == '！' || r2 == '？' {
				r = r2 // "…。" and "!？" end as 。 and ？ do, with nothing after them
			}
			e += n
		}
		for e < to {
			if k, ok := ends[e]; ok {
				e = k
				continue
			}
			r2, n := utf8.DecodeRuneInString(src[e:to])
			if !strings.ContainsRune(splitClosers, r2) || !quotes.in(e) && quotes.in(e+n) {
				break // not a closer, or a straight quote that opens the next sentence ("他说。"走吧")
			}
			e += n
		}
		if brackets.in(i) || brackets.in(e) || quotes.in(e) || !splitEnds(src, words, i, e, to, r, quotes.in(i)) {
			i = e
			continue
		}
		out = append(out, [2]int{start, e})
		start = splitSkipSpace(src, e, to, quote)
		words, i = start, start
	}
	if start < to {
		if end := splitTrimSpace(src, start, to); end > start {
			out = append(out, [2]int{start, end})
		}
	}
	return out
}

// splitEnds says whether the sentence from start ends after the mark r at
// i and the closers up to e, outside brackets and quotes. quoted is that i
// is inside quotes the closers closed.
//
// 。！？ end a sentence with no space after them, as Japanese and Chinese
// write ("東京です。大阪です。"), unless they close a quote the words go on
// after ("「そうだ。」と言った。"); no abbreviation, number or initial rule
// is theirs. The other marks need a space or the end after them.
func splitEnds(src string, start, i, e, to int, r rune, quoted bool) bool {
	wide := r == '。' || r == '！' || r == '？'
	if e < to && !unicode.IsSpace(splitRuneAt(src, e, to)) {
		return wide && !quoted
	}
	if wide {
		return true
	}
	next := splitSkipSpace(src, e, to, false)
	nr := splitRuneAt(src, next, to)
	if b := splitRuneBefore(src, i, start); nr >= 'a' && nr <= 'z' && b < 0x80 && mathLetter(byte(b)) {
		return false // "e.g. the", "approx. ten"; but "…입니다. iPhone" ends
	}
	if r != '.' || i+1 < to && src[i+1] == '.' {
		return true // not a period, or "...", an ellipsis
	}
	if splitDatePart(src, start, i, nr) {
		return false // "2026. 10. 30. 기준"
	}
	ws := i
	for ws > start && (mathLetter(src[ws-1]) || src[ws-1] == '.') {
		ws--
	}
	if ws < i && ws > start && splitWordRune(splitRuneBefore(src, ws, start)) {
		return true // "3Dr.": not a word of its own
	}
	w := src[ws:i]
	switch {
	case w == "":
	case splitAbbrev[strings.ToLower(w)]:
		return false
	case len(w) == 1 && w[0] >= 'A' && w[0] <= 'Z' && splitInitial(src, start, ws, next, to, nr):
		return false // an initial: "by J. K. Rowling", "Dr. J. Smith", but not "→ C. 다만" or "plan A. It"
	case w == "St", w == "No" && nr >= '0' && nr <= '9':
		return false
	}
	return !splitEnumerator(strings.TrimLeft(src[start:i], "*_")) && !splitLineNumber(src, start, i)
}

// splitInitial: the single capital at ws, with its period, is an initial,
// not a word of its own, when the word after it at next starts with nr.
// Another initial follows ("by J. K. Rowling"), or a capitalised word
// follows and the capital starts the sentence or follows an initial
// ("J. K."), a title ("Dr. J. Smith") or a capitalised word ("John F.").
// After an initial a word in another script may follow too ("J. K. 롤링").
func splitInitial(src string, start, ws, next, to int, nr rune) bool {
	capital := nr >= 'A' && nr <= 'Z'
	if capital && next+1 < to && src[next+1] == '.' && !splitWordRune(splitRuneAt(src, next+2, to)) {
		return true
	}
	p := ws
	for p > start && (src[p-1] == ' ' || src[p-1] == '\t') {
		p--
	}
	switch {
	case p == start:
		return capital
	case src[p-1] == '.':
		q := p - 1
		for q > start && mathLetter(src[q-1]) {
			q--
		}
		if w := src[q : p-1]; len(w) == 1 && w[0] >= 'A' && w[0] <= 'Z' {
			return capital || nr >= utf8.RuneSelf && unicode.IsLetter(nr)
		}
		return capital && splitTitles[strings.ToLower(src[q:p-1])]
	}
	q := p
	for q > start && mathLetter(src[q-1]) {
		q--
	}
	return capital && q < p && src[q] >= 'A' && src[q] <= 'Z'
}

// splitLineNumber: the period at i ends a number of one to three digits
// that starts a line of the paragraph (after a quote's ">" marks). It
// numbers what follows: an ordered marker other than "1." does not start a
// list inside a paragraph, so "설명 문단\n2. 둘째" is one sentence, as the
// app shows it.
func splitLineNumber(src string, start, i int) bool {
	d := i
	for d > start && i-d < 3 && mathDigit(src[d-1]) {
		d--
	}
	if d == i || d > start && mathDigit(src[d-1]) {
		return false
	}
	for d > start && (src[d-1] == ' ' || src[d-1] == '\t' || src[d-1] == '>') {
		d--
	}
	return d > start && (src[d-1] == '\n' || src[d-1] == '\r')
}

// splitDatePart: the period at i ends a number that is part of a date
// written with periods, as Korean does ("2026. 10. 30."): another number
// follows, or one came before it the same way. A compact date
// ("2025.10.30.") ends at its day; a version ("1.99.1.") is no date.
func splitDatePart(src string, start, i int, next rune) bool {
	d := i
	for d > start && mathDigit(src[d-1]) {
		d--
	}
	if d == i {
		return false
	}
	if next >= '0' && next <= '9' {
		return true
	}
	// "… 10. 30." : ". " and digits before this number
	if d-2 > start && src[d-1] == ' ' && src[d-2] == '.' && mathDigit(src[d-3]) {
		return true
	}
	// "2025.10.30.": a year of four digits, a month and a day of one or two
	if i-d > 2 || d <= start || src[d-1] != '.' {
		return false
	}
	m := d - 1
	for m > start && mathDigit(src[m-1]) {
		m--
	}
	if n := d - 1 - m; n < 1 || n > 2 || m <= start || src[m-1] != '.' {
		return false
	}
	y := m - 1
	for y > start && mathDigit(src[y-1]) {
		y--
	}
	return m-1-y == 4 && (y == start || src[y-1] != '.')
}

// splitEnumerator: a sentence so far that only numbers what follows ("1",
// "a", "iv").
func splitEnumerator(t string) bool {
	if t == "" || len(t) > 4 {
		return false
	}
	if strings.Trim(t, "0123456789") == "" && len(t) <= 3 {
		return true
	}
	if len(t) == 1 && mathLetter(t[0]) {
		return true
	}
	return strings.Trim(t, "ivxIVX") == ""
}

// splitSkipSpace moves i past white space, and in a quote past the ">"
// marks that start its lines.
func splitSkipSpace(src string, i, to int, quote bool) int {
	lineStart := true
	for i < to {
		r, size := utf8.DecodeRuneInString(src[i:to])
		switch {
		case r == '\n' || r == '\r':
			lineStart = true
		case unicode.IsSpace(r):
		case quote && lineStart && r == '>':
		default:
			return i
		}
		i += size
	}
	return i
}

// splitTrimSpace is b moved back over white space, not before a.
func splitTrimSpace(src string, a, b int) int {
	for b > a {
		r, size := utf8.DecodeLastRuneInString(src[a:b])
		if !unicode.IsSpace(r) {
			break
		}
		b -= size
	}
	return b
}

// splitCut cuts a span longer than MaxUnitChars characters: each piece
// ends at the last "; " or ", " within the limit, or else after its last
// 、，or ；(Japanese and Chinese, no space needed), or else at its last
// space, or else at the limit, never inside code, math or a link.
func splitCut(src string, a, b int, quote bool) [][2]int {
	guard, _ := splitProtect(splitInline(src, a, b, quote), a, b)
	var out [][2]int
	for cur := a; cur < b; {
		w := cur
		for count := 0; w < b && count < MaxUnitChars; count++ {
			_, size := utf8.DecodeRuneInString(src[w:b])
			w += size
		}
		if w >= b {
			out = append(out, [2]int{cur, b})
			break
		}
		cut := 0
		for k := w - 1; k > cur && cut == 0; k-- {
			if (src[k-1] == ';' || src[k-1] == ',') && (src[k] == ' ' || src[k] == '\t' || src[k] == '\n') && !guard.in(k-1) {
				cut = k
			}
		}
		for k := w; k > cur && cut == 0; k-- {
			if r, size := utf8.DecodeLastRuneInString(src[cur:k]); (r == '、' || r == '，' || r == '；') && k-size > cur && !guard.in(k-size) {
				cut = k
			}
		}
		low := cur // never right after an item's number: "1." alone is no unit
		if cur == a {
			low = splitNumberText(src, a, b)
		}
		for k := w - 1; k > low && cut == 0; k-- {
			if (src[k] == ' ' || src[k] == '\t' || src[k] == '\n') && !guard.in(k) {
				cut = k
			}
		}
		if cut == 0 {
			cut = w
		}
		if end := splitTrimSpace(src, cur, cut); end > cur {
			out = append(out, [2]int{cur, end})
		}
		cur = splitSkipSpace(src, cut, b, quote)
	}
	return out
}

// Inline marks.

// Strip is the text a unit shows: its raw Markdown with only the marks
// taken out. It removes ** __ ~~ and paired * and _ (not inside words or
// math), backticks (keeping the code), link targets ([t](u) is t, an image
// its alt text, <url> the url), footnote marks [^n], inline HTML tags and
// a quote's ">" marks, decodes entities and collapses white space. A row's
// cells are trimmed and joined by " | ", an escaped "\|" in them a "|" (in
// code and math too, as in GFM). Inline math stays as written, its
// white space collapsed: the phone draws it where Unit.Math says. A list
// marker or task box is no mark: Split's spans start after a bullet and
// its task box and at an ordered item's number, so "1. 서울" keeps its
// number and text that only looks like a marker stays.
func Strip(kind, raw string) string {
	text, _, _ := splitStrip(kind, raw)
	return text
}

// splitStrip is Strip, whether the text has content of its own, and where
// the text has math. Content is a letter or digit outside links,
// addresses, citations and a list number the span starts with ("1. "), so
// "1. [출처](https://…)" is only a link. math are the inline math tokens
// the scan read in raw (splitTokMath), as byte spans into text: not code,
// not an escaped "\$".
func splitStrip(kind, raw string) (text string, content bool, math []MathSpan) {
	if kind == UnitRow {
		// Cells joined by " | ", an empty one leaving "a | | c", not two
		// spaces or one at the end. Each cell's text is trimmed and its
		// white space collapsed, so this is the cells' words and "|" marks
		// joined by single spaces, and a cell's math moves by where it
		// starts.
		var b strings.Builder
		for k, cell := range splitCells(raw) {
			t, c, m := splitShow(cell, splitInline(cell, 0, len(cell), false))
			content = content || c
			if k > 0 {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteByte('|')
			}
			if t == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			for _, x := range m {
				math = append(math, MathSpan{Start: b.Len() + x.Start, End: b.Len() + x.End, Display: x.Display})
			}
			b.WriteString(t)
		}
		return b.String(), content, math
	}
	// A quote's ">" marks go with its line breaks (splitInline).
	quote := kind == UnitQuote
	text, content, math = splitShow(raw, splitInline(raw, 0, len(raw), quote))
	if n := splitNumberText(raw, 0, len(raw)); n > 0 && content {
		_, content, _ = splitShow(raw, splitInline(raw, n, len(raw), quote))
	}
	return text, content, math
}

// splitRowEnd is where the first cols cells of the table row [c, end)
// end, with the "|" after them: past them are cells the app (GFM) drops,
// as a "|" in "$|x|$" makes ("| 절댓값 | $|x|$ |" shows 절댓값 and $).
func splitRowEnd(t string, c, end, cols int) int {
	i, n := c, 0
	if i < end && t[i] == '|' {
		i++
	}
	for ; i < end; i++ {
		switch t[i] {
		case '\\':
			i++
		case '|':
			if n++; n == cols {
				return i + 1
			}
		}
	}
	return end
}

// splitCells are a table row's cells: split at each "|" that is not
// escaped, without the empty ones before the first "|" and after the last.
func splitCells(raw string) []string {
	s := strings.TrimSpace(raw)
	var cells []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '|':
			cells = append(cells, s[start:i])
			start = i + 1
		}
	}
	cells = append(cells, s[start:])
	if len(cells) > 1 && strings.HasPrefix(s, "|") {
		cells = cells[1:]
	}
	if n := len(cells); n > 1 && cells[n-1] == "" {
		cells = cells[:n-1]
	}
	// An escaped "|" is a "|" in the cell before its Markdown is read, as
	// in GFM and the app: in code and math too ("`a \| b`" is the code a | b).
	for k, cell := range cells {
		if strings.Contains(cell, `\|`) {
			cells[k] = strings.ReplaceAll(cell, `\|`, "|")
		}
	}
	return cells
}

// Kinds of inline tokens.
const (
	splitTokText  uint8 = iota // shows src[start:end]
	splitTokCode               // a code span: shows its code
	splitTokMath               // inline math: shows show, src[start:end] as written (in a quote without its ">" marks)
	splitTokURL                // a web address: shows show
	splitTokDrop               // shows nothing: link marks and targets, footnote marks, comments, styling tags, quote marks
	splitTokSpace              // shows one space: a line break, <br>
	splitTokChar               // an entity or an escaped character: shows show
	splitTokDelim              // a run of "*", "_" or "~": shown unless it is emphasis
	splitTokCite               // a citation like "[12]": shown as written, but no content
)

// splitTok is a piece of a line's Markdown.
type splitTok struct {
	kind        uint8
	start, end  int
	show        string
	link        int  // a link's opening mark: where the whole link ends
	inLink      bool // in a link's text: no content of its own
	foot        bool // a footnote mark
	display     bool // math that is "$$…$$" or "\[…\]"
	open, close bool // a delimiter run that may open or close emphasis
	keep        bool // a delimiter run that is shown
}

// splitScan reads the inline Markdown of src[from:to] in one pass, after
// two passes that pair backtick runs and brackets with stacks, so no mark
// is looked for twice.
type splitScan struct {
	src       string
	from, to  int
	quote     bool // a quote: ">" marks after line breaks go
	toks      []splitTok
	text      int         // where the text being read started, or -1
	code      map[int]int // a code span's start → its end
	square    map[int]int // "[" → its "]"
	paren     map[int]int // "(" → its ")"
	closes    []int       // the "]" of the links whose text is being read, innermost last
	targets   []int       // where their targets end
	gtFrom    int         // the first ">" at or after gtFrom is gt (-1: none)
	gt        int
	noComment bool    // no "-->" is left
	spaces    []int32 // white space before each offset from from, counted when first needed
}

func splitInline(src string, from, to int, quote bool) []splitTok {
	sc := &splitScan{src: src, from: from, to: to, quote: quote, text: -1, gtFrom: -1}
	sc.findCode()
	sc.findBrackets()
	i := from
	if quote {
		i = sc.quoteMarks(i)
	}
	for i < to {
		for n := len(sc.closes); n > 0 && i > sc.closes[n-1]; n-- {
			sc.closes, sc.targets = sc.closes[:n-1], sc.targets[:n-1] // passed inside code or math
		}
		if n := len(sc.closes); n > 0 && i == sc.closes[n-1] {
			sc.flush(i) // the link's text
			end := sc.targets[n-1]
			sc.closes, sc.targets = sc.closes[:n-1], sc.targets[:n-1]
			i = sc.put(splitTokDrop, i, end, "")
			continue
		}
		if j := sc.token(i); j > i {
			i = j
			continue
		}
		if sc.text < 0 {
			sc.text = i
		}
		i++
	}
	sc.flush(to)
	return sc.toks
}

func (sc *splitScan) flush(i int) {
	if sc.text >= 0 && i > sc.text {
		sc.toks = append(sc.toks, splitTok{kind: splitTokText, start: sc.text, end: i, inLink: len(sc.closes) > 0})
	}
	sc.text = -1
}

// put adds a token and gives its end.
func (sc *splitScan) put(kind uint8, start, end int, show string) int {
	sc.flush(start)
	sc.toks = append(sc.toks, splitTok{kind: kind, start: start, end: end, show: show, inLink: len(sc.closes) > 0})
	return end
}

// findCode pairs backtick runs as CommonMark does: a run opens code up to
// the next run of the same length, and inside code a backslash is only a
// backslash ("`C:\`" is the code C:\). A backslash escapes only the first
// backtick of a run that would open code: after an odd number of them
// the rest of the run opens (a backslash and two backticks show one
// backtick and open code with the other).
func (sc *splitScan) findCode() {
	type run struct{ at, n int }
	var runs []run
	for i := sc.from; i < sc.to; i++ {
		if sc.src[i] == '`' {
			j := i
			for j < sc.to && sc.src[j] == '`' {
				j++
			}
			runs = append(runs, run{i, j - i})
			i = j - 1
		}
	}
	if len(runs) < 2 {
		return
	}
	byLen := map[int][]int{}
	for k, r := range runs {
		byLen[r.n] = append(byLen[r.n], k)
	}
	next := map[int]int{}
	sc.code = map[int]int{}
	after := sc.from // the end of the last code: backslashes before it were code
	for k := 0; k < len(runs); k++ {
		at, n := runs[k].at, runs[k].n
		b := at
		for b > after && sc.src[b-1] == '\\' {
			b--
		}
		if (at-b)%2 == 1 {
			at, n = at+1, n-1 // its first backtick is escaped
		}
		if n == 0 {
			continue
		}
		list, p := byLen[n], next[n]
		for p < len(list) && list[p] <= k {
			p++
		}
		next[n] = p
		if p < len(list) {
			m := list[p]
			sc.code[at] = runs[m].at + runs[m].n
			after = sc.code[at]
			k = m
		}
	}
}

// findBrackets pairs "[" with "]" and "(" with ")" outside code.
func (sc *splitScan) findBrackets() {
	var sq, pa []int
	for i := sc.from; i < sc.to; i++ {
		switch sc.src[i] {
		case '\\':
			i++
		case '`':
			if e, ok := sc.code[i]; ok {
				i = e - 1
			}
		case '[':
			sq = append(sq, i)
		case ']':
			if n := len(sq); n > 0 {
				if sc.square == nil {
					sc.square = map[int]int{}
				}
				sc.square[sq[n-1]] = i
				sq = sq[:n-1]
			}
		case '(':
			pa = append(pa, i)
		case ')':
			if n := len(pa); n > 0 {
				if sc.paren == nil {
					sc.paren = map[int]int{}
				}
				sc.paren[pa[n-1]] = i
				pa = pa[:n-1]
			}
		}
	}
}

// token reads a mark at i and gives where it ends, or i for plain text.
func (sc *splitScan) token(i int) int {
	src, to := sc.src, sc.to
	switch src[i] {
	case '\\':
		if end, display, ok := mathAt(src[:to], i); ok {
			return sc.math(i, end, display)
		}
		if i+1 < to && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", src[i+1]) >= 0 {
			return sc.put(splitTokChar, i, i+2, src[i+1:i+2])
		}
	case '$':
		if end, display, ok := mathAt(src[:to], i); ok {
			return sc.math(i, end, display)
		}
	case '`':
		j := i
		for j < to && src[j] == '`' {
			j++
		}
		if e, ok := sc.code[i]; ok {
			return sc.put(splitTokCode, i, e, sc.unquote(src[j:e-(j-i)]))
		}
		if sc.text < 0 {
			sc.text = i
		}
		return j // backticks that open no code are text
	case '!':
		if i+1 < to && src[i+1] == '[' {
			if c, end, ok := sc.link(i + 1); ok {
				return sc.openLink(i, i+2, c, end)
			}
		}
	case '[':
		j, ok := sc.square[i]
		if !ok {
			break
		}
		if j > i+2 && src[i+1] == '^' && !sc.spaceIn(i+2, j) {
			end := sc.put(splitTokDrop, i, j+1, "")
			sc.toks[len(sc.toks)-1].foot = true
			return end
		}
		if c, end, ok := sc.link(i); ok {
			return sc.openLink(i, i+1, c, end)
		}
		if splitCiteMark(src, i, j) {
			return sc.put(splitTokCite, i, j+1, "")
		}
	case '<':
		return sc.angle(i)
	case '&':
		if e, show := splitEntity(src, i, to); e > 0 {
			return sc.put(splitTokChar, i, e, show)
		}
	case '*', '_', '~':
		return sc.delim(i)
	case '\n', '\r':
		j := i + 1
		if src[i] == '\r' && j < to && src[j] == '\n' {
			j++
		}
		sc.put(splitTokSpace, i, j, " ")
		if sc.quote {
			j = sc.quoteMarks(j)
		}
		return j
	case 'h', 'H', 'w', 'W':
		if e := splitURLAt(src, i, sc.from, to); e > i {
			return sc.put(splitTokURL, i, e, src[i:e])
		}
	}
	return i
}

// math adds the inline math from i to end and gives its end.
func (sc *splitScan) math(i, end int, display bool) int {
	sc.put(splitTokMath, i, end, sc.unquote(sc.src[i:end]))
	sc.toks[len(sc.toks)-1].display = display
	return end
}

// spaceIn reports whether src[a:b] holds white space. It counts white
// space once, the first time it is asked, so nested "[^" marks are each
// checked at once and the scan stays linear.
func (sc *splitScan) spaceIn(a, b int) bool {
	if sc.spaces == nil {
		sc.spaces = make([]int32, sc.to-sc.from+1)
		for k := sc.from; k < sc.to; k++ {
			n := sc.spaces[k-sc.from]
			if c := sc.src[k]; c == ' ' || c == '\t' || c == '\r' || c == '\n' {
				n++
			}
			sc.spaces[k-sc.from+1] = n
		}
	}
	return sc.spaces[b-sc.from] > sc.spaces[a-sc.from]
}

// link reads the link whose text opens with the "[" at i: [t](u) or
// [t][ref]. closeAt is its "]" and end where its target ends. [t][ref] is
// a link only by a definition, which a span cannot see, so it is none
// where it is more often text: right after a letter or digit ("A[0][1]")
// or with a citation number for t ("근거[1][2]").
func (sc *splitScan) link(i int) (closeAt, end int, ok bool) {
	j, ok := sc.square[i]
	if !ok || j+1 >= sc.to {
		return 0, 0, false
	}
	switch sc.src[j+1] {
	case '(':
		if k, ok := sc.paren[j+1]; ok {
			return j, k + 1, true
		}
	case '[':
		if splitWordRune(splitRuneBefore(sc.src, i, sc.from)) || splitCiteMark(sc.src, i, j) {
			return 0, 0, false
		}
		if k, ok := sc.square[j+1]; ok {
			return j, k + 1, true
		}
	}
	return 0, 0, false
}

// splitCiteMark: the brackets at i and j hold a citation number of one to
// three digits ("[12]").
func splitCiteMark(src string, i, j int) bool {
	return j-i >= 2 && j-i <= 4 && strings.Trim(src[i+1:j], "0123456789") == ""
}

func (sc *splitScan) openLink(i, text, closeAt, end int) int {
	sc.put(splitTokDrop, i, text, "")
	sc.toks[len(sc.toks)-1].link = end
	sc.closes = append(sc.closes, closeAt)
	sc.targets = append(sc.targets, end)
	return text
}

// angle reads what starts with "<": a comment, an address, a styling tag
// or "<br>". Other HTML ("List<T>") is text, as the app shows it.
func (sc *splitScan) angle(i int) int {
	src, to := sc.src, sc.to
	if !sc.noComment && strings.HasPrefix(src[i:to], "<!--") {
		if k := strings.Index(src[i+4:to], "-->"); k >= 0 {
			return sc.put(splitTokDrop, i, i+4+k+3, "")
		}
		sc.noComment = true
	}
	if sc.gtFrom < 0 || i+1 < sc.gtFrom || sc.gt >= 0 && i+1 > sc.gt {
		sc.gtFrom, sc.gt = i+1, -1
		if k := strings.IndexByte(src[i+1:to], '>'); k >= 0 {
			sc.gt = i + 1 + k
		}
	}
	if sc.gt < 0 {
		return i
	}
	inner := src[i+1 : sc.gt]
	if splitAutolink(inner) {
		return sc.put(splitTokURL, i, sc.gt+1, inner)
	}
	switch splitTagName(inner) {
	case "br":
		return sc.put(splitTokSpace, i, sc.gt+1, " ")
	case "a", "b", "i", "u", "em", "strong", "sup", "sub", "span", "small", "mark", "kbd", "ins", "del", "s", "details", "summary":
		return sc.put(splitTokDrop, i, sc.gt+1, "")
	}
	return i
}

// delim reads a run of "*", "_" or "~" and whether it may open or close
// emphasis (CommonMark's flanking rules).
func (sc *splitScan) delim(i int) int {
	src, to := sc.src, sc.to
	j := i
	for j < to && src[j] == src[i] {
		j++
	}
	before, after := splitRuneBefore(src, i, sc.from), splitRuneAt(src, j, to)
	left := !unicode.IsSpace(after) && (!splitPunct(after) || unicode.IsSpace(before) || splitPunct(before))
	right := !unicode.IsSpace(before) && (!splitPunct(before) || unicode.IsSpace(after) || splitPunct(after))
	sc.put(splitTokDelim, i, j, "")
	t := &sc.toks[len(sc.toks)-1]
	t.open, t.close = left, right
	if src[i] == '_' {
		t.open = left && (!right || splitPunct(before))
		t.close = right && (!left || splitPunct(after))
	}
	return j
}

// unquote is code or math of a quote without the ">" marks and spaces that
// start its lines after the first, as the app gets it: "`git\n> log`" in
// a quote is the code git log.
func (sc *splitScan) unquote(s string) string {
	if !sc.quote || !strings.ContainsAny(s, "\r\n") {
		return s
	}
	var b strings.Builder
	lineStart := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\n' || c == '\r':
			lineStart = true
		case lineStart && (c == ' ' || c == '\t' || c == '>'):
			continue
		default:
			lineStart = false
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// quoteMarks drops the spaces and ">" marks that start a quote's line.
func (sc *splitScan) quoteMarks(i int) int {
	j := i
	for j < sc.to && (sc.src[j] == ' ' || sc.src[j] == '\t' || sc.src[j] == '>') {
		j++
	}
	if j > i {
		sc.put(splitTokDrop, i, j, "")
	}
	return j
}

func splitPunct(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

// splitASCIIWordAt: src[i] is an ASCII letter or digit.
func splitASCIIWordAt(src string, i int) bool {
	return i >= 0 && i < len(src) && (mathLetter(src[i]) || mathDigit(src[i]))
}

// splitShow is what the tokens show, white space collapsed, and whether a
// letter or digit of it is the unit's own (not in a link, an address or a
// citation). Runs of "**", "__" (not inside a word) and "~~" are marks; a
// single "*" or "_" is one when it pairs with a later one. A run of "*" or
// "~" is no mark when the app shows it as written: with spaces on both
// sides ("2 ** 10"), or between ASCII letters or digits with no other such
// run in the unit to pair with ("2**10", "1~~2"). Next to another letter,
// as Hangul, such a run closes or opens bold whose other end is in another
// sentence of the paragraph ("**첫 문장입니다. 둘째**는"), so it is a mark.
//
// It also gives where the text has math, as byte spans into the text: each
// inline math token, as written with its white space collapsed. A token
// starts and ends with its marks, never white space, so a space before it
// goes before the span and none is left pending after it.
func splitShow(src string, toks []splitTok) (string, bool, []MathSpan) {
	var stars, unders []int
	runs := [2]int{} // runs of two or more "*", and of "~"
	run := func(c byte) *int {
		if c == '~' {
			return &runs[1]
		}
		return &runs[0]
	}
	for _, t := range toks {
		if t.kind == splitTokDelim && t.end-t.start >= 2 && src[t.start] != '_' {
			*run(src[t.start])++
		}
	}
	for k := range toks {
		t := &toks[k]
		if t.kind != splitTokDelim {
			continue
		}
		t.keep = true
		c, n := src[t.start], t.end-t.start
		switch {
		case n >= 2 && c != '_':
			t.keep = !t.open && !t.close || t.open && t.close && *run(c) == 1 && splitASCIIWordAt(src, t.start-1) && splitASCIIWordAt(src, t.end)
		case n >= 2:
			t.keep = !t.open && !t.close
		case c == '~':
		default:
			stack := &stars
			if c == '_' {
				stack = &unders
			}
			if m := len(*stack); t.close && m > 0 {
				toks[(*stack)[m-1]].keep = false
				t.keep = false
				*stack = (*stack)[:m-1]
			} else if t.open {
				*stack = append(*stack, k)
			}
		}
	}
	var b strings.Builder
	var math []MathSpan
	content, space := false, false
	write := func(s string, own bool) {
		for _, r := range s {
			if unicode.IsSpace(r) {
				space = b.Len() > 0
				continue
			}
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteRune(r)
			if own && !content && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				content = true
			}
		}
	}
	for _, t := range toks {
		switch t.kind {
		case splitTokText:
			write(src[t.start:t.end], !t.inLink)
		case splitTokDelim:
			if t.keep {
				write(src[t.start:t.end], !t.inLink)
			}
		case splitTokCode, splitTokChar:
			write(t.show, !t.inLink)
		case splitTokMath:
			if space {
				b.WriteByte(' ')
				space = false
			}
			start := b.Len()
			write(t.show, !t.inLink)
			math = append(math, MathSpan{Start: start, End: b.Len(), Display: t.display})
		case splitTokURL:
			write(t.show, false)
		case splitTokCite:
			write(src[t.start:t.end], false)
		case splitTokSpace:
			write(" ", false)
		}
	}
	return b.String(), content, math
}

// splitURLAt reads a web address at i ("https://…", "www.…") that starts
// a word: up to white space or "<", without the punctuation after it or
// a ")" it did not open. It gives i when there is none.
func splitURLAt(src string, i, from, to int) int {
	rest := src[i:to]
	n := 0
	for _, p := range []string{"https://", "http://", "www."} {
		if len(rest) >= len(p) && strings.EqualFold(rest[:len(p)], p) {
			n = len(p)
			break
		}
	}
	if n == 0 || splitWordRune(splitRuneBefore(src, i, from)) {
		return i
	}
	e, opens, closes := i+n, 0, 0
	for e < to {
		r, size := utf8.DecodeRuneInString(src[e:to])
		if unicode.IsSpace(r) || r == '<' {
			break
		}
		switch r {
		case '(':
			opens++
		case ')':
			closes++
		}
		e += size
	}
	for e > i+n {
		r, size := utf8.DecodeLastRuneInString(src[i:e])
		if r == ')' && closes > opens {
			closes--
		} else if !strings.ContainsRune(".,:;!?\"'*_~", r) {
			break
		}
		e -= size
	}
	if e <= i+n {
		return i
	}
	return e
}

// splitAutolink: what is inside "<…>" is a web or mail address.
func splitAutolink(inner string) bool {
	if strings.ContainsAny(inner, " \t\r\n<") {
		return false
	}
	for _, p := range []string{"https://", "http://", "mailto:", "ftp://"} {
		if len(inner) > len(p) && strings.EqualFold(inner[:len(p)], p) {
			return true
		}
	}
	return false
}

// splitTagName is the lower-case name of the HTML tag inside "<…>", or "".
func splitTagName(inner string) string {
	s := strings.TrimPrefix(inner, "/")
	n := 0
	for n < len(s) && (mathLetter(s[n]) || n > 0 && mathDigit(s[n])) {
		n++
	}
	if n == 0 {
		return ""
	}
	if rest := s[n:]; rest != "" && rest != "/" && rest[0] != ' ' && rest[0] != '\t' && rest[0] != '\n' {
		return ""
	}
	return strings.ToLower(s[:n])
}

// splitEntity reads an entity at i ("&amp;", "&#8212;", "&#x2014;") and
// gives where it ends and its character, or 0 when there is none known.
// The names are the app's (Markdown.tsx).
func splitEntity(src string, i, to int) (int, string) {
	j := i + 1
	if j < to && src[j] == '#' {
		j++
		base, most := 10, 7
		if j < to && (src[j] == 'x' || src[j] == 'X') {
			base, most = 16, 6
			j++
		}
		k := j
		for k < to && k-j <= most && (mathDigit(src[k]) || base == 16 && strings.IndexByte("abcdefABCDEF", src[k]) >= 0) {
			k++
		}
		if k == j || k-j > most || k >= to || src[k] != ';' {
			return 0, ""
		}
		n, err := strconv.ParseInt(src[j:k], base, 32)
		if err != nil || n <= 0 || n > unicode.MaxRune || n >= 0xD800 && n <= 0xDFFF {
			return k + 1, "\xef\xbf\xbd"
		}
		return k + 1, string(rune(n))
	}
	k := j
	for k < to && k-j <= 32 && (mathLetter(src[k]) || mathDigit(src[k])) {
		k++
	}
	if k-j < 2 || k-j > 32 || k >= to || src[k] != ';' || !mathLetter(src[j]) {
		return 0, ""
	}
	if v, ok := splitEntities[src[j:k]]; ok {
		return k + 1, v
	}
	return 0, ""
}

var splitEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'", "nbsp": "\xc2\xa0", "ensp": "\xe2\x80\x82",
	"emsp": "\xe2\x80\x83", "thinsp": "\xe2\x80\x89", "shy": "\xc2\xad", "zwj": "\xe2\x80\x8d", "zwnj": "\xe2\x80\x8c", "copy": "©",
	"reg": "®", "trade": "™", "mdash": "—", "ndash": "–", "hellip": "…", "laquo": "«", "raquo": "»",
	"lsquo": "‘", "rsquo": "’", "ldquo": "“", "rdquo": "”", "bull": "•", "middot": "·", "deg": "°",
	"plusmn": "±", "times": "×", "divide": "÷", "minus": "−", "le": "≤", "ge": "≥", "ne": "≠",
	"asymp": "≈", "equiv": "≡", "sim": "∼", "infin": "∞", "larr": "←", "rarr": "→", "uarr": "↑",
	"darr": "↓", "harr": "↔", "lArr": "⇐", "rArr": "⇒", "hArr": "⇔", "crarr": "↵", "sect": "§",
	"para": "¶", "cent": "¢", "pound": "£", "euro": "€", "yen": "¥", "curren": "¤", "frac12": "½",
	"frac14": "¼", "frac34": "¾", "sup1": "¹", "sup2": "²", "sup3": "³", "micro": "µ", "permil": "‰",
	"prime": "′", "Prime": "″", "dagger": "†", "Dagger": "‡", "iexcl": "¡", "iquest": "¿", "check": "✓",
	"cross": "✗", "star": "☆", "starf": "★", "hearts": "♥", "spades": "♠", "clubs": "♣", "diams": "♦",
	"sum": "∑", "prod": "∏", "radic": "√", "part": "∂", "nabla": "∇", "isin": "∈", "notin": "∉",
	"and": "∧", "or": "∨", "cap": "∩", "cup": "∪", "int": "∫", "there4": "∴", "sub": "⊂", "sup": "⊃",
	"empty": "∅", "forall": "∀", "exist": "∃", "lowast": "∗", "alpha": "α", "beta": "β", "gamma": "γ",
	"delta": "δ", "epsilon": "ε", "theta": "θ", "lambda": "λ", "mu": "μ", "pi": "π", "sigma": "σ",
	"tau": "τ", "phi": "φ", "omega": "ω", "Delta": "Δ", "Sigma": "Σ", "Omega": "Ω", "Pi": "Π",
}

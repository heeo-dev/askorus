package judge

import (
	"strings"
	"unicode/utf8"
)

// Inline math (R-24). The app finds math written inside a line with mathAt
// in app/src/ui/Markdown.tsx (moving to src/ui/inlineMath.ts in S5); this
// is the same rule, character for character, counted in UTF-16 code units
// as JavaScript counts them. Both run docs/judge/testdata/inline-math.json.
//
// "$" is also money and shell, so a "$" opens math only before a non-space
// and not right after a digit or "$" ("5$", "$$$"), and the first "$"
// after it must close it: after a non-space that is not an opening bracket
// or a currency ("(US$)"), and not before a digit or "$" (pandoc's rule,
// so "$5에서 $10" stays money). Math stops at a backtick; a number followed
// by Korean ("$20이고") is money, and so is a run of English words. "$$" in
// a line needs letters or digits too. "\(…\)" needs a command, an operator
// or a short name, and "\[…\]" in a line a command or "^": a backslash
// before a bracket is also Markdown's escape ("\[options\]").

// mathMaxInline is how far, in UTF-16 code units, a span's closing mark may
// be from its start, as in the app (maxInlineMath); it keeps every scan
// short.
const mathMaxInline = 2000

// InlineMath finds the math written inside a line of text ("$…$",
// "$$…$$", "\(…\)" and "\[…\]") by the app's rule (R-24): mathAt in
// app/src/ui/Markdown.tsx, which moves to src/ui/inlineMath.ts. A "$" is
// money or shell unless pandoc's rule, with the app's money, currency,
// prose and code exceptions, makes it math. The app and Go run the same
// cases, docs/judge/testdata/inline-math.json. Spans are in order and do
// not overlap.
//
// The scan is the app's maskMath loop: at each "$" or "\", mathAt with the
// character before it; after a span the scan goes on at its end, and a "\"
// that starts no math also hides the character after it.
func InlineMath(text string) []MathSpan {
	var spans []MathSpan
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != '$' && c != '\\' {
			continue
		}
		if end, display, ok := mathAt(text, i); ok {
			spans = append(spans, MathSpan{Start: i, End: end, Display: display})
			i = end - 1
			continue
		}
		if c == '\\' {
			i++ // an escaped character; a byte inside a character is never "$" or "\"
		}
	}
	return spans
}

// mathAt reads math that starts at src[at] and gives the byte offset after
// it and whether it is display math ("$$…$$", "\[…\]"). The character
// before it is src[at-1]: in UTF-8 a byte before at that is "$" or a digit
// is that character, as the code unit before it is in JavaScript.
func mathAt(src string, at int) (end int, display, ok bool) {
	if src[at] == '\\' {
		return mathBackslash(src, at)
	}
	if src[at] != '$' {
		return 0, false, false
	}
	if at > 0 && (src[at-1] == '$' || mathDigit(src[at-1])) {
		return 0, false, false
	}
	n := 1
	if at+1 < len(src) && src[at+1] == '$' {
		n = 2
	}
	if at+n >= len(src) {
		return 0, false, false
	}
	if r, _ := utf8.DecodeRuneInString(src[at+n:]); r == '$' || mathSpace(r) {
		return 0, false, false
	}
	for i, units := at+n, n; i < len(src) && units <= mathMaxInline; {
		switch src[i] {
		case '\\':
			// the backslash and the character after it
			i, units = i+1, units+1
			if i < len(src) {
				_, size, u := mathRune(src, i)
				i, units = i+size, units+u
			}
		case '`':
			return 0, false, false // code binds tighter
		case '$':
			if n == 2 && (i+1 >= len(src) || src[i+1] != '$') {
				return 0, false, false
			}
			if !mathDollarOK(src, at, i, n) {
				return 0, false, false
			}
			return i + n, n == 2, true
		default:
			_, size, u := mathRune(src, i)
			i, units = i+size, units+u
		}
	}
	return 0, false, false
}

// mathBackslash reads "\(…\)" or "\[…\]" at src[at].
func mathBackslash(src string, at int) (end int, display, ok bool) {
	if at+1 >= len(src) {
		return 0, false, false
	}
	open := src[at+1]
	var close byte
	switch open {
	case '(':
		close = ')'
	case '[':
		close = ']'
	default:
		return 0, false, false
	}
	for i, units := at+2, 2; i < len(src) && units <= mathMaxInline; {
		if src[i] == '`' {
			return 0, false, false // code binds tighter
		}
		if src[i] != '\\' {
			_, size, u := mathRune(src, i)
			i, units = i+size, units+u
			continue
		}
		if i+1 < len(src) {
			switch src[i+1] {
			case '\\':
				i, units = i+2, units+2 // "\\", a line break, as in "\\[4pt]"
				continue
			case open:
				return 0, false, false // a second opening first: not this one
			case close:
				text := mathTrim(src[at+2 : i])
				if open == '(' {
					ok = strings.ContainsAny(text, `\^_=+<>`) || mathShortName(text)
				} else {
					ok = mathHasCommand(text) || strings.IndexByte(text, '^') >= 0
				}
				if !ok {
					return 0, false, false
				}
				return i + 2, open == '[', true
			}
		}
		i, units = i+1, units+1
	}
	return 0, false, false
}

// mathDollarOK says whether "$…$" or "$$…$$" from src[at] to the closing
// mark at src[i] is math and not money, shell or prose.
func mathDollarOK(src string, at, i, n int) bool {
	text := src[at+n : i] // not empty: the opening is followed by a non-space, non-"$"
	if r, _ := utf8.DecodeLastRuneInString(text); mathSpace(r) || r == '(' || r == '[' || r == '{' {
		return false
	}
	if mathCurrency(text) {
		return false
	}
	if j := i + n; j < len(src) && (src[j] == '$' || mathDigit(src[j])) {
		return false
	}
	// "**$HOME** and **$PATH**": the next bold's marks, not the end of math
	j := len(text)
	for j > 0 && (text[j-1] == '*' || text[j-1] == '_') {
		j--
	}
	if j < len(text) && j > 0 {
		if r, _ := utf8.DecodeLastRuneInString(text[:j]); mathSpace(r) {
			return false
		}
	}
	k := 0
	for k < len(text) && (text[k] == '*' || text[k] == '_') {
		k++
	}
	if k > 0 && k < len(text) {
		if r, _ := utf8.DecodeRuneInString(text[k:]); mathSpace(r) {
			return false
		}
	}
	bare := mathBare(text)
	if mathProse(bare) {
		return false
	}
	if n == 2 {
		return mathLettersOrDigits(text) && !mathKorean(bare)
	}
	return mathy(text) && !mathMoneyWord(text)
}

// mathCurrency: the text ends with a currency code after a space or "("
// ("(US$)", "in HK$").
func mathCurrency(text string) bool {
	if len(text) < 3 {
		return false
	}
	switch text[len(text)-2:] {
	case "US", "HK", "NT", "NZ", "AU", "CA", "SG":
	default:
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:len(text)-2])
	return mathSpace(r) || r == '('
}

// mathMoneyWord: a number followed by Korean, as in "$20이고".
func mathMoneyWord(text string) bool {
	k := 0
	for k < len(text) && (mathDigit(text[k]) || text[k] == ',' || text[k] == '.') {
		k++
	}
	if k == 0 {
		return false
	}
	for k < len(text) {
		r, size := utf8.DecodeRuneInString(text[k:])
		if mathSpace(r) {
			k += size
			continue
		}
		return mathHangul(r)
	}
	return false
}

// mathy: something to show, not only marks ("$x$", "$5$", "$α$", "$넓이$",
// but not "$–$").
func mathy(tex string) bool {
	for _, r := range tex {
		if mathAlnum(r) || mathGreek(r) || mathHangul(r) || strings.ContainsRune(`\^_=+<>*/|`, r) {
			return true
		}
	}
	return false
}

func mathLettersOrDigits(tex string) bool {
	for _, r := range tex {
		if mathAlnum(r) || mathGreek(r) {
			return true
		}
	}
	return false
}

func mathKorean(bare string) bool {
	for _, r := range bare {
		if mathHangul(r) {
			return true
		}
	}
	return false
}

// mathShortName: one to three ASCII letters or digits, like x or ab1.
func mathShortName(text string) bool {
	if len(text) < 1 || len(text) > 3 {
		return false
	}
	for i := 0; i < len(text); i++ {
		if !mathAlnum(rune(text[i])) {
			return false
		}
	}
	return true
}

// mathHasCommand: a backslash before an ASCII letter.
func mathHasCommand(text string) bool {
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '\\' && mathLetter(text[i+1]) {
			return true
		}
	}
	return false
}

// mathBare is what is left of TeX once \text{…}, \math…{…},
// \operatorname{…}, \mbox{…} and then every other command are taken out,
// each becoming a space.
func mathBare(tex string) string {
	if strings.IndexByte(tex, '\\') < 0 {
		return tex
	}
	var b strings.Builder
	for i := 0; i < len(tex); {
		if tex[i] == '\\' {
			if j, ok := mathTextCommand(tex, i); ok {
				b.WriteByte(' ')
				i = j
				continue
			}
		}
		b.WriteByte(tex[i])
		i++
	}
	s := b.String()
	b.Reset()
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) && mathLetter(s[i+1]) {
			j := i + 1
			for j < len(s) && mathLetter(s[j]) {
				j++
			}
			b.WriteByte(' ')
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// mathTextCommand reads \text…{…}, \math…{…}, \operatorname{…} or
// \mbox{…} (no braces inside) at tex[i] and gives the offset after it.
func mathTextCommand(tex string, i int) (int, bool) {
	rest := tex[i+1:]
	var j int
	switch {
	case strings.HasPrefix(rest, "text"), strings.HasPrefix(rest, "math"):
		j = i + 5
		for j < len(tex) && mathWordByte(tex[j]) {
			j++
		}
	case strings.HasPrefix(rest, "operatorname"):
		j = i + 13
	case strings.HasPrefix(rest, "mbox"):
		j = i + 5
	default:
		return 0, false
	}
	for j < len(tex) {
		r, size := utf8.DecodeRuneInString(tex[j:])
		if !mathSpace(r) {
			break
		}
		j += size
	}
	if j >= len(tex) || tex[j] != '{' {
		return 0, false
	}
	for k := j + 1; k < len(tex); k++ {
		switch tex[k] {
		case '}':
			return k + 1, true
		case '{':
			return 0, false
		}
	}
	return 0, false
}

// mathProse: three English words in a row, as in "$10 and Pro is 20$".
// bare is the text after mathBare.
func mathProse(bare string) bool {
	prev := rune(-1)
	for p := 0; p < len(bare); {
		r, size := utf8.DecodeRuneInString(bare[p:])
		if (p == 0 || mathSpace(prev) || prev == '(') && mathThreeWords(bare, p) {
			return true
		}
		prev = r
		p += size
	}
	return false
}

// mathThreeWords: s[p:] starts with three words of two or more ASCII
// letters, with white space between, and the third ends at white space,
// ")", ",", ".", ";", ":" or the end.
func mathThreeWords(s string, p int) bool {
	j := p
	for w := 0; w < 3; w++ {
		k := j
		for k < len(s) && mathLetter(s[k]) {
			k++
		}
		if k-j < 2 {
			return false
		}
		j = k
		if w == 2 {
			break
		}
		m := j
		for m < len(s) {
			r, size := utf8.DecodeRuneInString(s[m:])
			if !mathSpace(r) {
				break
			}
			m += size
		}
		if m == j {
			return false
		}
		j = m
	}
	if j == len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[j:])
	return mathSpace(r) || strings.ContainsRune("),.;:", r)
}

// mathSpace is JavaScript's \s (and what String.prototype.trim takes off).
func mathSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// mathTrim is JavaScript's String.prototype.trim.
func mathTrim(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if !mathSpace(r) {
			break
		}
		s = s[size:]
	}
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if !mathSpace(r) {
			break
		}
		s = s[:len(s)-size]
	}
	return s
}

func mathDigit(c byte) bool    { return c >= '0' && c <= '9' }
func mathLetter(c byte) bool   { return c|0x20 >= 'a' && c|0x20 <= 'z' }
func mathWordByte(c byte) bool { return mathLetter(c) || mathDigit(c) || c == '_' }
func mathAlnum(r rune) bool    { return r < 0x80 && (mathLetter(byte(r)) || mathDigit(byte(r))) }
func mathGreek(r rune) bool    { return r >= 0x370 && r <= 0x3FF }
func mathHangul(r rune) bool   { return r >= 0xAC00 && r <= 0xD7A3 }

// UTF-16 offsets, as the phone counts them.

// mathRune is the character at s[i], its size in bytes and its length in
// UTF-16 code units. A byte that starts no valid character is one U+FFFD.
func mathRune(s string, i int) (rune, int, int) {
	r, size := utf8.DecodeRuneInString(s[i:])
	if r >= 0x10000 {
		return r, size, 2
	}
	return r, size, 1
}

// UTF16Offset is the UTF-16 offset of byte offset b in s: what JavaScript
// counts on the phone. b is clamped to [0, len(s)]; a b inside a
// character counts to that character's start.
func UTF16Offset(s string, b int) int {
	b = min(max(b, 0), len(s))
	u := 0
	for i := 0; i < b; {
		_, size, n := mathRune(s, i)
		if i+size > b {
			break
		}
		i += size
		u += n
	}
	return u
}

// ByteOffset is the byte offset in s of UTF-16 offset u, clamped to s. An
// offset inside a character gives that character's start.
func ByteOffset(s string, u int) int {
	if u <= 0 {
		return 0
	}
	n := 0
	for i := 0; i < len(s); {
		_, size, w := mathRune(s, i)
		if n+w > u {
			return i
		}
		n += w
		i += size
		if n == u {
			return i
		}
	}
	return len(s)
}

// SliceUTF16 is s between the UTF-16 offsets start and end, as
// s.slice(start, end) gives it on the phone (a negative offset counts from
// the end). An offset inside a character moves to that character's start.
func SliceUTF16(s string, start, end int) string {
	total := UTF16Offset(s, len(s))
	at := func(x int) int {
		if x < 0 {
			return max(total+x, 0)
		}
		return min(x, total)
	}
	start, end = at(start), at(end)
	if start >= end {
		return ""
	}
	return s[ByteOffset(s, start):ByteOffset(s, end)]
}

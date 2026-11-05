package judge

import (
	"strings"
	"unicode/utf8"
)

// Helpers of the prompts (synth.go, refine.go): the data block's content
// as JSON strings, the earlier questions, cuts at character boundaries and
// the nonce checks. The judge prompt (judge-v1) that once used them too
// went with judging (C6d, #24).

// promptEarlier keeps the last MaxEarlier earlier questions that are not
// blank, oldest first, each cut to MaxEarlierBytes of UTF-8 at a
// character boundary. The phone sends no more than that; this holds even
// if it did.
func promptEarlier(earlier []string) []string {
	var out []string
	for _, q := range earlier {
		if strings.TrimSpace(q) != "" {
			out = append(out, promptClip(q, MaxEarlierBytes))
		}
	}
	if len(out) > MaxEarlier {
		out = out[len(out)-MaxEarlier:]
	}
	return out
}

// promptClip cuts s to at most n bytes, before the character that would
// be cut in two.
func promptClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for k := 0; k < utf8.UTFMax-1 && n > 0 && !utf8.RuneStart(s[n]); k++ {
		n--
	}
	return s[:n]
}

// promptNonceTries bounds the draws of Nonce. Each draw has 64 random
// bits, so a second one is all but never needed.
const promptNonceTries = 8

// promptOccurs reports whether nonce occurs in content.
func promptOccurs(content []string, nonce string) bool {
	for _, c := range content {
		if strings.Contains(c, nonce) {
			return true
		}
	}
	return false
}

// promptNonceOK reports whether n has the form Nonce draws.
func promptNonceOK(n string) bool {
	if len(n) != 16 {
		return false
	}
	for i := 0; i < len(n); i++ {
		if c := n[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// promptQuote writes s as a JSON string. Unlike encoding/json it leaves
// <, > and & as they are, so answers read as the AIs wrote them, and it
// escapes every control character, C1 ones included, and U+2028 and
// U+2029, so a string never breaks its line. A byte that is not UTF-8
// becomes U+FFFD.
func promptQuote(b *strings.Builder, s string) {
	const hexDigits = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20, r >= 0x7f && r <= 0x9f, r == '\u2028', r == '\u2029':
			b.WriteString(`\u`)
			for shift := 12; shift >= 0; shift -= 4 {
				b.WriteByte(hexDigits[r>>shift&0xf])
			}
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}

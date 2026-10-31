package judge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// mathFixture is docs/judge/testdata/inline-math.json, which the app's
// jest test runs too (S5): spans in UTF-16 code units.
type mathFixture struct {
	About string `json:"about"`
	Cases []struct {
		Text string `json:"text"`
		Math []struct {
			Start   int  `json:"start"`
			End     int  `json:"end"`
			Display bool `json:"display"`
		} `json:"math"`
	} `json:"cases"`
}

func TestInlineMathSharedCases(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(p06Dir, "testdata", "inline-math.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f mathFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) < 40 {
		t.Fatalf("%d cases", len(f.Cases))
	}
	found := 0
	for i, c := range f.Cases {
		var got []string
		for _, s := range InlineMath(c.Text) {
			got = append(got, mathSpanString(UTF16Offset(c.Text, s.Start), UTF16Offset(c.Text, s.End), s.Display))
		}
		var want []string
		for _, s := range c.Math {
			want = append(want, mathSpanString(s.Start, s.End, s.Display))
		}
		found += len(want)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("case %d %q:\n got %v\nwant %v", i, mathClip(c.Text), got, want)
		}
	}
	if found < 25 {
		t.Errorf("only %d spans in the shared cases", found)
	}
}

func mathSpanString(start, end int, display bool) string {
	b, _ := json.Marshal([]any{start, end, display})
	return string(b)
}

func mathClip(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:60]) + "…" + string(r[len(r)-10:])
	}
	return s
}

// Byte offsets of the spans, and the marks they hold.
func TestInlineMathBytes(t *testing.T) {
	text := "넓이 $\\pi r^2$, 식 \\(a\\neq0\\), 공식 $$x^2$$ 끝"
	spans := InlineMath(text)
	var got []string
	for _, s := range spans {
		got = append(got, text[s.Start:s.End])
	}
	want := []string{"$\\pi r^2$", "\\(a\\neq0\\)", "$$x^2$$"}
	if strings.Join(got, "|") != strings.Join(want, "|") || !spans[2].Display || spans[0].Display {
		t.Errorf("spans %+v = %q, want %q", spans, got, want)
	}
}

// Text the app was hurt by stays quick: every scan stops at the next mark.
func TestInlineMathStaysLinear(t *testing.T) {
	for name, text := range map[string]string{
		"dollars":       strings.Repeat("$a ", 20000),
		"double":        strings.Repeat("$$a\n", 15000),
		"open paren":    strings.Repeat("\\(x ", 15000),
		"open bracket":  strings.Repeat("\\[a ", 15000),
		"mixed":         strings.Repeat("\\(\\x\\[\\y$", 8000),
		"no closing":    "$" + strings.Repeat("x", 60000),
		"backslashes":   strings.Repeat("\\\\", 30000),
		"money and ks":  strings.Repeat("$20이고 ", 8000),
		"prose in math": "$" + strings.Repeat("ab cd ef ", 220) + "$",
	} {
		start := time.Now()
		InlineMath(text)
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Errorf("%s: %v", name, d)
		}
	}
}

func TestUTF16Offsets(t *testing.T) {
	s := "a😀한\u00e9b" // 1 + 2 + 1 + 1 + 1 code units; 1 + 4 + 3 + 2 + 1 bytes
	units := utf16.Encode([]rune(s))
	if len(units) != 6 {
		t.Fatalf("%d units", len(units))
	}
	for _, c := range []struct{ b, u int }{
		{0, 0}, {1, 1}, {5, 3}, {8, 4}, {10, 5}, {11, 6},
		{3, 1}, {6, 3}, // inside a character: its start
		{-4, 0}, {99, 6}, // clamped
	} {
		if got := UTF16Offset(s, c.b); got != c.u {
			t.Errorf("UTF16Offset(%d) = %d, want %d", c.b, got, c.u)
		}
	}
	for _, c := range []struct{ u, b int }{
		{0, 0}, {1, 1}, {3, 5}, {4, 8}, {5, 10}, {6, 11},
		{2, 1}, // inside the surrogate pair: the emoji's start
		{-1, 0}, {7, 11},
	} {
		if got := ByteOffset(s, c.u); got != c.b {
			t.Errorf("ByteOffset(%d) = %d, want %d", c.u, got, c.b)
		}
	}
	for _, c := range []struct {
		start, end int
		want       string
	}{
		{0, 6, s}, {1, 3, "😀"}, {3, 5, "한\u00e9"}, {4, 2, ""}, {-2, 99, "\u00e9b"}, {5, -1, ""},
	} {
		if got := SliceUTF16(s, c.start, c.end); got != c.want {
			t.Errorf("SliceUTF16(%d, %d) = %q, want %q", c.start, c.end, got, c.want)
		}
	}
	// A byte that starts no character counts as one U+FFFD, as it does
	// once the text is JSON on the phone.
	bad := "a\xffb"
	if UTF16Offset(bad, 3) != 3 || ByteOffset(bad, 2) != 2 || SliceUTF16(bad, 2, 3) != "b" {
		t.Errorf("invalid UTF-8: %d %d %q", UTF16Offset(bad, 3), ByteOffset(bad, 2), SliceUTF16(bad, 2, 3))
	}
	// At every character start the offset is the UTF-16 length of the text
	// before it: what the phone counts for a NumberSpan (aggregate).
	for _, s := range []string{"", "plain", "출시일은 2026년", "🍎 a😀b", "a\xffb\xe2\x82", "éé한😀"} {
		for b := -1; b <= len(s)+1; b++ {
			if b > 0 && b < len(s) && !utf8.RuneStart(s[b]) {
				continue // inside a character: checked above
			}
			want := len(utf16.Encode([]rune(s[:min(max(b, 0), len(s))])))
			if got := UTF16Offset(s, b); got != want {
				t.Errorf("UTF16Offset(%q, %d) = %d, want %d", s, b, got, want)
			}
		}
	}
}

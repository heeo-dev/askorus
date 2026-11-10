package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// R-95: while pair waits for a phone, "c" + Enter copies the link (in a
// classic console window the mouse cannot select it, R-90). Anything else
// typed then is dropped and copies nothing.
func TestWaitingLineCopiesTheLink(t *testing.T) {
	if !clipboardSupported() {
		t.Skip("no clipboard on this system yet")
	}
	const link = "askorus://pair?code=ABCDEFGHIJKLMNOPQRSTUVWXYZ&host=pc.example.ts.net&port=8800&v=1"
	var copied []string
	old := copyLink
	copyLink = func(s string) error { copied = append(copied, s); return nil }
	t.Cleanup(func() { copyLink = old })

	for _, line := range []string{"c", "C", " c ", "ㅊ"} {
		var out bytes.Buffer
		copied = nil
		waitingLine(line, link, &out)
		if len(copied) != 1 || copied[0] != link {
			t.Errorf("%q: copied %q", line, copied)
		}
		if !strings.Contains(out.String(), "copied the link to the clipboard") || !strings.Contains(out.String(), "링크를 클립보드에 복사했습니다") {
			t.Errorf("%q: said %q", line, out.String())
		}
	}
	for _, line := range []string{"", "y", "n", "cc", "copy", "askorus"} {
		var out bytes.Buffer
		copied = nil
		waitingLine(line, link, &out)
		if len(copied) != 0 || out.Len() != 0 {
			t.Errorf("%q: copied %q, said %q", line, copied, out.String())
		}
	}

	// A clipboard that refuses says so and pair goes on.
	copyLink = func(string) error { return errors.New("the clipboard is in use by another program") }
	var out bytes.Buffer
	waitingLine("c", link, &out)
	if !strings.Contains(out.String(), "could not copy the link") || !strings.Contains(out.String(), "링크를 복사하지 못했습니다") {
		t.Errorf("a refused copy said %q", out.String())
	}
}

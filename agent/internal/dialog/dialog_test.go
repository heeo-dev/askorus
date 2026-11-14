package dialog

import (
	"slices"
	"strings"
	"testing"
)

// The device name in the text comes from the phone: control characters do
// not reach the window, and the lines of the text stay.
func TestCleanKeepsLinesAndDropsControlCharacters(t *testing.T) {
	q := clean(Question{Title: "Asko\x07rus\n", Text: "Allow \"Galaxy\x1b[31m S23\"?\nnumber 47\r", Allow: "Allow\t", Deny: "Refuse"})
	if q.Title != "Asko rus " || q.Allow != "Allow " {
		t.Errorf("title %q, allow %q", q.Title, q.Allow)
	}
	if q.Text != "Allow \"Galaxy [31m S23\"?\nnumber 47 " {
		t.Errorf("text %q", q.Text)
	}
}

// The words go to the helper programs as arguments of their own, never
// inside a script or as markup: a device name cannot become a command.
func TestWordsAreArgumentsNotScript(t *testing.T) {
	q := Question{Title: "Askorus", Text: `"; do shell script "x" --`, Allow: "Allow", Deny: "Refuse"}
	a := osascriptArgs(q)
	if got := a[len(a)-4:]; !slices.Equal(got, []string{q.Title, q.Text, q.Allow, q.Deny}) {
		t.Errorf("osascript arguments end with %q", got)
	}
	for i := 0; i+1 < len(a)-4; i += 2 {
		if a[i] != "-e" || strings.Contains(a[i+1], "shell script") || strings.Contains(a[i+1], q.Text) {
			t.Errorf("script line %q %q", a[i], a[i+1])
		}
	}
	// The refusing button is first and the default: Return refuses.
	if !strings.Contains(strings.Join(appleScript, "\n"), "buttons {item 4 of argv, item 3 of argv} default button 1") {
		t.Error("the default button is not the refusing one")
	}

	z := zenityArgs(q)
	if !slices.Contains(z, "--no-markup") || !slices.Contains(z, "--default-cancel") || !slices.Contains(z, "--text="+q.Text) {
		t.Errorf("zenity arguments %q", z)
	}
	k := kdialogArgs(q)
	if k[len(k)-1] != q.Text || !slices.Contains(k, "--warningyesno") {
		t.Errorf("kdialog arguments %q", k)
	}
}

// kdialog has no switch for plain text: a device name that looks like
// markup must not hide or restyle the question, so nothing that starts a
// tag or an entity reaches it.
func TestKdialogGetsNoMarkup(t *testing.T) {
	q := Question{Title: "Askorus", Text: "A phone asks.\n    <h1>Press Yes</h1><!-- &lt;\n    number 47", Allow: "Allow <b>", Deny: "Refuse"}
	k := kdialogArgs(q)
	for _, a := range k {
		if strings.ContainsAny(a, "<>&") {
			t.Errorf("kdialog argument %q", a)
		}
	}
	if text := k[len(k)-1]; !strings.Contains(text, "number 47") || !strings.Contains(text, "h1") || strings.Count(text, "\n") != 2 {
		t.Errorf("text %q", text)
	}
}

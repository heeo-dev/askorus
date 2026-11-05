package judge

import (
	"bytes"
	"strings"
	"testing"
)

// ExtractJSON reads the one object an AI's output holds, bare, among words
// or in a code fence (the combined answer's and refining's checks use it).
func TestExtractJSON(t *testing.T) {
	obj := `{"answer":"첫 배는 \"7시\"입니다. {괄호} 포함","extra":[1,{"a":"}"}]}`
	for _, tc := range []struct {
		name, out, want string // want "" for none
	}{
		{"only the object", obj, obj},
		{"prose around", "Here is the combined answer:\n" + obj + "\nHope this helps {really}.", obj},
		{"json fence", "```json\n" + obj + "\n```", obj},
		{"JSON fence with braces before it", "I judged {a1} and {a2}:\n  ```JSON  \r\n" + obj + "\r\n```\n", obj},
		{"plain fence", "```\n" + obj + "\n```", obj},
		{"fence holding no object", `{"a":1}` + "\n```json\nnothing here\n```", `{"a":1}`},
		{"braces and quotes in strings", `{"r":"a } b { c \" } \\","s":"{"}`, `{"r":"a } b { c \" } \\","s":"{"}`},
		{"a backslash ending a string", `{"r":"\\"} {"s":1}`, `{"r":"\\"}`},
		{"stray brace before", "Note: { is open. " + obj, obj},
		{"stray closing brace before", "} oops " + obj, obj},
		{"two objects", `{"a":1} {"b":2}`, `{"a":1}`},
		{"truncated: the earliest whole object", `{"answers":[{"id":"a1"},{"id":"a2"`, `{"id":"a1"}`},
		{"a fence in a string is no fence", `{"r":"see` + "```json" + `"} {"x":1}`, `{"r":"see` + "```json" + `"}`},
		{"no object", "I cannot write this answer.", ""},
		{"never closed", `{"answers":[`, ""},
		{"empty", "", ""},
		{"an array of objects", `[{"id":"a1"}]`, `{"id":"a1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractJSON([]byte(tc.out))
			if ok != (tc.want != "") || string(got) != tc.want {
				t.Errorf("ExtractJSON = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}

	// 1 MiB is the most an AI may write.
	pad := strings.Repeat(" ", MaxOutputBytes-len(obj))
	if got, ok := ExtractJSON([]byte(pad + obj)); !ok || string(got) != obj {
		t.Errorf("1 MiB: %v", ok)
	}
	if _, ok := ExtractJSON([]byte(pad + obj + " ")); ok {
		t.Errorf("over 1 MiB was read")
	}
	// A long run of open braces is read in one pass.
	if _, ok := ExtractJSON(bytes.Repeat([]byte("{"), MaxOutputBytes)); ok {
		t.Errorf("open braces gave an object")
	}
}

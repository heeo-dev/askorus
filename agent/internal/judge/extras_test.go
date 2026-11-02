package judge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The cases are shared with the app's src/ask/extras-test.ts.
func TestExtrasStart(t *testing.T) {
	data, err := os.ReadFile("testdata/extras.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Text, Main string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 10 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, c := range cases {
		if got := c.Text[:ExtrasStart(c.Text)]; got != c.Main {
			t.Errorf("%s:\n got %q\nwant %q", c.Name, got, c.Main)
		}
	}
}

// R-61 (#4): what follows the cut is never judged, and the rest of the
// answer splits as before.
func TestSplitLeavesExtrasOut(t *testing.T) {
	body := "핸들이 흔들리면 헤드셋을 보세요. 포크가 휘었을 수도 있습니다."
	text := body + "\n\n**다음에 쓸 문장**\n```\n포크 휨 확인하는 법 알려줘\n```\n\n원하시면 더 알려 드릴게요."
	units, tail, _ := SplitAnswer("opencode", text, 0)
	want, _, _ := SplitAnswer("opencode", body, 0)
	if len(units) != len(want) {
		t.Fatalf("%d units, want %d: %+v", len(units), len(want), units)
	}
	for i := range units {
		if units[i].Text != want[i].Text || units[i].Start != want[i].Start || units[i].End != want[i].End {
			t.Errorf("unit %d = %+v, want %+v", i, units[i], want[i])
		}
	}
	for _, l := range tail {
		if strings.Contains(l, "다음에 쓸 문장") || strings.Contains(l, "포크 휨") {
			t.Errorf("tail keeps the extras: %q", l)
		}
	}
}

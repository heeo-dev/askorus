package judge

import (
	"encoding/json"
	"os"
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

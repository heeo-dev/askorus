//go:build syntheval

package main

import (
	"testing"

	"github.com/asksemble/asksemble/agent/internal/judge"
)

// A quoted table row keeps its bars, and an empty part before the quote
// (an extra "|") is not a failure: both broke two replies of run c5-x.
func TestParseDiffersBars(t *testing.T) {
	answers := []judge.SynthAnswer{{AI: "Claude", Text: "| 모델 | 가격 |\n| M4 | 70~90만 원 |"}, {AI: "Codex", Text: "중앙값은 112만 원입니다."}}
	got, ok := parseDiffers("PLACE: M4 중고 시세\nSIDE: Claude | 70~90만 원 | | M4 | 70~90만 원 |\nSIDE: Codex | 112만 원 | | 중앙값은 112만 원입니다.", answers)
	if !ok || len(got) != 1 || got[0].Sides[0].Quote != "M4 | 70~90만 원" || !got[0].Sides[0].Found || got[0].Sides[1].Quote != "중앙값은 112만 원입니다." || !got[0].Sides[1].Found {
		t.Fatalf("got %+v %v", got, ok)
	}
}

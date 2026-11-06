package core

import (
	"reflect"
	"testing"
)

// 11-06: the app's own pick of model and effort lists efforts least to most.
func TestSortEfforts(t *testing.T) {
	got := SortEfforts([]string{"max", "", "low", "turbo", "high", "low", "ultra", "minimal"})
	if want := []string{"minimal", "low", "high", "max", "ultra", "turbo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SortEfforts = %v, want %v", got, want)
	}
	if got := SortEfforts(nil); got == nil || len(got) != 0 {
		t.Errorf("SortEfforts(nil) = %#v, want an empty list (JSON [])", got)
	}
}

func TestTierEfforts(t *testing.T) {
	tiers := map[string]AskModel{
		TierQuick:    {Model: "known", Effort: "low"},
		TierStandard: {Model: "other", Effort: "high"},
		TierDeep:     {Model: "other", Effort: "medium"},
	}
	got := TierEfforts(tiers, func(m string) []string {
		if m == "known" {
			return []string{"max", "low"}
		}
		return nil
	})
	want := map[string][]string{
		"known": {"low", "max"},
		// A model the adapter has no list for offers what its tiers use.
		"other": {"medium", "high"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TierEfforts = %v, want %v", got, want)
	}
}

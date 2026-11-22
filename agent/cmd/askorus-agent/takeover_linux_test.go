//go:build linux

package main

import (
	"os"
	"testing"
)

// R-177 (#71): which control groups are the agent's unit.
func TestCgroupInUnit(t *testing.T) {
	for text, want := range map[string]bool{
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/askorus-agent.service\n":              true,
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/askorus-agent.service/child\n":        true,
		"12:pids:/user.slice\n1:name=systemd:/user.slice/user@1000.service/askorus-agent.service\n0::/\n": true,
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/run-r909a.scope\n":                    false,
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/my-askorus-agent.service\n":           false,
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/askorus-agent.service.d\n":            false,
		"0::/init.scope\n":      false,
		"":                      false,
		"askorus-agent.service": false,
	} {
		if got := cgroupInUnit(text, unitName); got != want {
			t.Errorf("cgroupInUnit(%q) = %v, want %v", text, got, want)
		}
	}
}

// The mark of a moved update-finish never reaches the agent it starts.
func TestLeaveServiceTakesItsMarkAway(t *testing.T) {
	if inService(os.Getpid()) {
		t.Skip("the tests run inside the agent's unit")
	}
	t.Setenv(scopedEnv, "1")
	if err := leaveService(); err != nil {
		t.Fatal(err)
	}
	if v, ok := os.LookupEnv(scopedEnv); ok {
		t.Errorf("%s=%q is left", scopedEnv, v)
	}
}

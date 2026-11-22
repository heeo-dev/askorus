//go:build linux

package main

import (
	"os"
	"slices"
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

// R-181: a "$" in the path is never replaced by systemd-run.
func TestScopeArgs(t *testing.T) {
	args := []string{updateFinishArg, "123", "0.6.2", "0.6.3", "1"}
	plain := scopeArgs("/home/a b/.local/bin/askorus-agent", args)
	want := append([]string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--", "/home/a b/.local/bin/askorus-agent"}, args...)
	if !slices.Equal(plain, want) {
		t.Errorf("scopeArgs = %q", plain)
	}
	odd := scopeArgs("/home/${USER}/askorus-agent", args)
	i, j := slices.Index(odd, "--expand-environment=no"), slices.Index(odd, "--")
	if i < 0 || i > j || odd[j+1] != "/home/${USER}/askorus-agent" {
		t.Errorf("scopeArgs with a $ = %q", odd)
	}
}

package proc

import (
	"errors"
	"runtime"
	"testing"
)

// R-118 (#62): an agent started over SSH on a Mac is outside the screen's
// session, where Claude Code's sign-in in the keychain cannot be read.
func TestOutsideScreenOf(t *testing.T) {
	none := func(string) string { return "" }
	ssh := func(k string) string {
		if k == "SSH_CONNECTION" {
			return "10.0.0.2 50000 10.0.0.1 22"
		}
		return ""
	}
	failed := errors.New("launchctl: not found")
	cases := []struct {
		name    string
		manager string
		err     error
		env     func(string) string
		want    bool
	}{
		{"terminal on the screen", "Aqua\n", nil, none, false},
		{"screen wins over an SSH variable", "Aqua\n", nil, ssh, false},
		{"over SSH", "Background\n", nil, none, true},
		{"standard IO", "StandardIO\n", nil, none, true},
		{"login window", "LoginWindow", nil, none, true},
		{"launchctl failed, SSH variables", "", failed, ssh, true},
		{"launchctl failed, no SSH", "", failed, none, false},
		{"empty answer, no SSH", "  \n", nil, none, false},
	}
	for _, c := range cases {
		if got := outsideScreenOf(c.manager, c.err, c.env); got != c.want {
			t.Errorf("%s: outsideScreenOf(%q) = %v, want %v", c.name, c.manager, got, c.want)
		}
	}
}

func TestOutsideScreenSessionOnlyOnMac(t *testing.T) {
	if runtime.GOOS != "darwin" && OutsideScreenSession() {
		t.Error("OutsideScreenSession is true off macOS")
	}
}

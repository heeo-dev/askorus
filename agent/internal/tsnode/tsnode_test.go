package tsnode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The node's device name is made from the PC's name: only what a DNS label
// may hold, never empty, never too long.
func TestHostName(t *testing.T) {
	for in, want := range map[string]string{
		"DESKTOP-AB12CD":   "askorus-desktop-ab12cd",
		"My PC":            "askorus-my-pc",
		"mac.local":        "askorus-mac-local",
		"집 컴퓨터":            "askorus-pc",
		"":                 "askorus-pc",
		"--x--":            "askorus-x",
		"a_b":              "askorus-a-b",
		"name;rm -rf /":    "askorus-namerm--rf",
		"UPPER_and.lower-": "askorus-upper-and-lower",
	} {
		if got := HostName(in); got != want {
			t.Errorf("HostName(%q) = %q, want %q", in, got, want)
		}
	}
	long := HostName(strings.Repeat("abcdefgh-", 20))
	if len(long) > 63 || strings.HasSuffix(long, "-") || !strings.HasPrefix(long, "askorus-") {
		t.Errorf("long name = %q (%d)", long, len(long))
	}
}

// A binary built without the tags would carry the library's service for
// other devices, which opens a TCP port on the PC's own network interfaces
// (#57 review): the node does not start in it.
func TestStartNeedsBuildTags(t *testing.T) {
	if builtRight() {
		return // built as the agent is: nothing to refuse (and no node is started in tests)
	}
	if _, err := Start(t.TempDir(), "askorus-test", nil); !errors.Is(err, ErrBuildTags) {
		t.Errorf("Start in a binary without the tags = %v, want ErrBuildTags", err)
	}
}

// Every place that builds the agent or the app's bridge names the same
// tags, so none of them makes a binary that refuses to start its node (or,
// were the check gone, one that opens the port).
func TestBuildTagsEverywhere(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "tsbridge", "tsbridge.go")); err != nil {
		t.Skip("the agent's folder alone, not the whole repository")
	}
	for _, f := range []string{
		"scripts/release.ps1", "scripts/test-all.ps1", "scripts/test-unix.sh", "scripts/test-install.sh",
		".github/workflows/agent-unix.yml", "tsbridge/build-android.sh", "tsbridge/tsbridge.go",
		"app/scripts/go-libs.mjs", "README.md", "README.ko.md",
	} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if !strings.Contains(string(b), BuildTags) {
			t.Errorf("%s does not name the build tags %s", f, BuildTags)
		}
	}
}

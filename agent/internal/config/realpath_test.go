package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// #52, found by the first macOS run: the temporary folder there is behind
// a link (/var is /private/var), the AI programs record the real path, and
// the ask folder's conversations then looked like another folder's. The ask
// folder is given by its real path, also before the first ask has made it.
func TestAskFolderIsTheRealPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		c := Default()
		c.Ask.Folder = filepath.Join(t.TempDir(), "asks")
		if got, _ := c.AskFolder(); got != c.Ask.Folder {
			t.Errorf("windows: ask folder = %q, want it as it is", got)
		}
		return
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real, link := filepath.Join(root, "real"), filepath.Join(root, "link")
	if err := os.MkdirAll(filepath.Join(real, "made"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		filepath.Join(link, "made"):              filepath.Join(real, "made"),
		filepath.Join(link, "not", "yet", "ask"): filepath.Join(real, "not", "yet", "ask"),
		filepath.Join(real, "made"):              filepath.Join(real, "made"),
		filepath.Join(root, "nothing", "here"):   filepath.Join(root, "nothing", "here"),
	} {
		c := Default()
		c.Ask.Folder = in
		if got, err := c.AskFolder(); err != nil || got != want {
			t.Errorf("ask folder %q = %q, %v, want %q", in, got, err, want)
		}
	}
}

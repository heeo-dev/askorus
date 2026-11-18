package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// R-139 (#35 review): a junction outside a checkout that leads into it does
// not let a key folder end up inside the repository.
func TestKeysStayOutOfRepositoriesThroughAJunction(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "inner")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, inside).CombinedOutput(); err != nil {
		t.Skipf("no junction here: %v %s", err, out)
	}
	k := newKeys(t)
	for _, dir := range []string{link, filepath.Join(link, "keys")} {
		k.typed = []string{pw, pw}
		if _, err := k.run("root-new", dir); err == nil || !strings.Contains(err.Error(), "outside any repository") {
			t.Errorf("root-new %s: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(inside, rootKeyFile)); err == nil {
		t.Fatal("a root key was written into the repository")
	}
}

package tsbridge

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FetchFile (#14): a 2xx answer goes into a new file of the app's own, never
// over a file that is there, never into the node's folder, and never larger
// than maxSaveFile.
func TestFetchFileWritesOnlyANewFileOutsideTheNodesFolder(t *testing.T) {
	dir := t.TempDir()
	pics := filepath.Join(dir, "files", "pictures")
	os.MkdirAll(pics, 0o700)
	state := filepath.Join(dir, "files", "tailnet")
	os.MkdirAll(state, 0o700)
	key := filepath.Join(state, "tailscaled.state")
	os.WriteFile(key, []byte("secret key"), 0o600)
	mu.Lock()
	old := stateDir
	stateDir = state
	mu.Unlock()
	defer func() {
		mu.Lock()
		stateDir = old
		mu.Unlock()
	}()

	fresh := filepath.Join(pics, "img_1.png")
	if err := checkNewFile(fresh); err != nil {
		t.Fatalf("a new file in the app's folder: %v", err)
	}
	there := filepath.Join(pics, "there.png")
	os.WriteFile(there, []byte("kept"), 0o600)
	sep := string(filepath.Separator)
	for name, path := range map[string]string{
		"relative":          "img.png",
		"not clean":         pics + sep + ".." + sep + "pictures" + sep + "img.png",
		"a file is there":   there,
		"the node's key":    key,
		"in the node's dir": filepath.Join(state, "new.png"),
		"no such folder":    filepath.Join(pics, "nope", "img.png"),
		"empty":             "",
	} {
		if err := checkNewFile(path); errorCode(err) != CodeBadRequest {
			t.Errorf("%s: %v", name, err)
		}
	}
	link := filepath.Join(dir, "files", "into-state")
	if err := os.Symlink(state, link); err == nil {
		if err := checkNewFile(filepath.Join(link, "new.png")); errorCode(err) != CodeBadRequest {
			t.Errorf("the node's folder through a link: %v", err)
		}
	}
	if b, _ := os.ReadFile(there); string(b) != "kept" {
		t.Error("a file that was there changed")
	}

	// saveBody writes what came, and leaves nothing when it fails.
	data := bytes.Repeat([]byte{7}, 1000)
	if err := saveBody(bytes.NewReader(data), fresh); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(fresh); !bytes.Equal(b, data) {
		t.Error("the file is not the answer")
	}
	if err := saveBody(bytes.NewReader(data), fresh); errorCode(err) != CodeBadRequest {
		t.Errorf("written over a file: %v", err)
	}
	big := filepath.Join(pics, "big.png")
	if err := saveBody(bytes.NewReader(make([]byte, maxSaveFile+1)), big); errorCode(err) != CodeTooLarge {
		t.Errorf("too large: %v", err)
	}
	if _, err := os.Stat(big); !errors.Is(err, os.ErrNotExist) {
		t.Error("the part of a too large answer was left")
	}
	empty := filepath.Join(pics, "empty.png")
	if err := saveBody(strings.NewReader(""), empty); errorCode(err) != CodeBadRequest {
		t.Errorf("empty: %v", err)
	}
	if _, err := os.Stat(empty); !errors.Is(err, os.ErrNotExist) {
		t.Error("an empty file was left")
	}
	fits := filepath.Join(pics, "fits.png")
	if err := saveBody(bytes.NewReader(make([]byte, maxSaveFile)), fits); err != nil {
		t.Errorf("exactly the limit: %v", err)
	}
}

func TestFetchFileNeedsTheNode(t *testing.T) {
	mu.Lock()
	hc := httpc
	mu.Unlock()
	if hc != nil {
		t.Skip("a node is running")
	}
	path := filepath.Join(t.TempDir(), "img.png")
	if _, err := FetchFile("", "GET", "http://pc.example.ts.net:8790/v1/runs/r/images/i", "{}", path, 1000); errorCode(err) != CodeNotStarted {
		t.Errorf("without a node: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("a file was made without a node")
	}
}

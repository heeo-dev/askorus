package tsbridge

import (
	"os"
	"path/filepath"
	"testing"
)

// SendFile (#14) opens only a real file of a sane size, by a clean absolute
// path, and never one of the node's own folder: the node's key is there.
func TestSendFileOpensOnlyWhatItMaySend(t *testing.T) {
	dir := t.TempDir()
	photo := filepath.Join(dir, "cache", "photo.jpg")
	os.MkdirAll(filepath.Dir(photo), 0o700)
	if err := os.WriteFile(photo, []byte{0xFF, 0xD8, 0xFF, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	f, size, err := openOwnFile(photo)
	if err != nil || size != 6 {
		t.Fatalf("a photo: size %d, %v", size, err)
	}
	f.Close()

	empty := filepath.Join(dir, "cache", "empty.jpg")
	os.WriteFile(empty, nil, 0o600)
	big := filepath.Join(dir, "cache", "big.jpg")
	os.WriteFile(big, make([]byte, maxSendFile+1), 0o600)
	fits := filepath.Join(dir, "cache", "fits.jpg")
	os.WriteFile(fits, make([]byte, maxSendFile), 0o600)
	if f, _, err := openOwnFile(fits); err != nil {
		t.Errorf("a file of exactly the limit: %v", err)
	} else {
		f.Close()
	}
	sep := string(filepath.Separator)
	for name, c := range map[string]struct{ path, code string }{
		"relative":       {"photo.jpg", CodeBadRequest},
		"not clean":      {dir + sep + "cache" + sep + ".." + sep + "cache" + sep + "photo.jpg", CodeBadRequest},
		"missing":        {filepath.Join(dir, "cache", "none.jpg"), CodeBadRequest},
		"a folder":       {filepath.Join(dir, "cache"), CodeBadRequest},
		"empty":          {empty, CodeBadRequest},
		"too large":      {big, CodeTooLarge},
		"no path at all": {"", CodeBadRequest},
	} {
		if f, _, err := openOwnFile(c.path); errorCode(err) != c.code {
			if f != nil {
				f.Close()
			}
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}

	// The node's own folder: its key, by its path or through a link.
	state := filepath.Join(dir, "files", "tailnet")
	os.MkdirAll(filepath.Join(state, "sub"), 0o700)
	key := filepath.Join(state, "sub", "tailscaled.state")
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
	if f, _, err := openOwnFile(key); errorCode(err) != CodeBadRequest {
		if f != nil {
			f.Close()
		}
		t.Errorf("the node's key was opened: %v", err)
	}
	link := filepath.Join(dir, "cache", "link.jpg")
	if err := os.Symlink(key, link); err == nil {
		if f, _, err := openOwnFile(link); errorCode(err) != CodeBadRequest {
			if f != nil {
				f.Close()
			}
			t.Errorf("the node's key was opened through a link: %v", err)
		}
	}
	// A file beside the node's folder, even one whose name starts the same, goes.
	beside := filepath.Join(dir, "files", "tailnet-photos.jpg")
	os.WriteFile(beside, []byte("x"), 0o600)
	if f, _, err := openOwnFile(beside); err != nil {
		t.Errorf("a file beside the node's folder: %v", err)
	} else {
		f.Close()
	}
	if f, _, err := openOwnFile(photo); err != nil {
		t.Errorf("a photo with the node started: %v", err)
	} else {
		f.Close()
	}

	// Nothing is sent before the node is started, and the file check comes first.
	if _, err := SendFile("POST", "http://askorus-pc.tail0.ts.net:8790/v1/uploads", "", photo, 1000); errorCode(err) != CodeNotStarted {
		t.Errorf("not started: %v", err)
	}
	if _, err := SendFile("POST", "http://askorus-pc.tail0.ts.net:8790/v1/uploads", "", key, 1000); errorCode(err) != CodeBadRequest {
		t.Errorf("the node's key, not started: %v", err)
	}
}

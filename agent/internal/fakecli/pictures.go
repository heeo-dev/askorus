package fakecli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// Made pictures (#14). A fake that has its picture tool in an ask makes
// ASKORUS_FAKE_PICTURES pictures (none by default) where its AI saves them
// and names the first one's whole path in its answer, as the real AIs did
// when asked where they saved it: codex PNGs in
// $CODEX_HOME/generated_images/<thread>/exec-<uuid>.png (never without
// CODEX_HOME, so a test cannot write into the person's own folder), agy
// JPEGs in its brain folder (agy.go). Each picture is written a moment
// after the one before, so their times keep their order.
const EnvPictures = "ASKORUS_FAKE_PICTURES"

// Fake picture bytes: a PNG of 1024x1024 and a JPEG of 800x600, by their
// first bytes, as core.ImageSize reads them.
var (
	FakePNG  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 4, 0, 0, 0, 4, 0}, bytes.Repeat([]byte{9}, 200)...)
	FakeJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 4, 1, 1, 0xFF, 0xC0, 0, 11, 8, 0x02, 0x58, 0x03, 0x20, 1, 1, 0x11, 0}, bytes.Repeat([]byte{9}, 200)...)
)

func picturesWanted() int {
	n, err := strconv.Atoi(os.Getenv(EnvPictures))
	if err != nil || n < 0 {
		return 0
	}
	return min(n, 8)
}

// writePictures makes n pictures in dir by name(i) and returns the first
// one's path, or "".
func writePictures(dir string, n int, name func(i int) string, data []byte) string {
	if n == 0 || os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	first := ""
	for i := range n {
		p := filepath.Join(dir, name(i))
		if os.WriteFile(p, data, 0o600) != nil {
			return first
		}
		at := time.Now().Add(time.Duration(i-n) * time.Second)
		_ = os.Chtimes(p, at, at)
		if first == "" {
			first = p
		}
	}
	return first
}

// codexPictures makes the pictures of a codex ask whose --disable list
// leaves image_generation on, and returns what the answer says of them.
func codexPictures(c *Call, thread string) string {
	home := os.Getenv("CODEX_HOME")
	if slices.Contains(c.Args, "image_generation") || home == "" || !filepath.IsAbs(home) {
		return ""
	}
	first := writePictures(filepath.Join(home, "generated_images", thread), picturesWanted(),
		func(int) string { return "exec-" + newUUID() + ".png" }, FakePNG)
	if first == "" {
		return ""
	}
	return " [saved: " + first + "]"
}

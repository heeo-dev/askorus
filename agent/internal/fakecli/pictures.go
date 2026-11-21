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

// EnvAgyBrain is the folder the fake agy keeps its conversations' files in
// (agy's own is ~/.gemini/antigravity-cli/brain); the fake makes no picture
// without it. The adapter's tests give the same folder as Options.BrainDir.
const EnvAgyBrain = "ASKORUS_FAKE_AGY_BRAIN"

// PicturesLimit as ASKORUS_FAKE_PICTURES: agy's picture tool is at its
// limit; the step fails with Google's 429 and the time it refreshes.
const PicturesLimit = "limit"

// AgyPictureReset is when the fake agy's picture limit refreshes, and
// AgyPictureLimitError the tool's error as agy 1.5.10 printed it on
// 2026-11-20 (shortened).
const (
	AgyPictureReset      = "2026-11-24T16:26:01Z"
	AgyPictureLimitError = "image failed to generate: failed to generate content: 429 Too Many Requests, body: {\n  \"error\": {\n    \"code\": 429,\n    \"message\": \"You have exhausted your capacity on this model.\",\n    \"status\": \"RESOURCE_EXHAUSTED\",\n    \"details\": [\n      {\n        \"@type\": \"type.googleapis.com/google.rpc.ErrorInfo\",\n        \"reason\": \"QUOTA_EXHAUSTED\",\n        \"metadata\": {\n          \"model\": \"gemini-3.1-flash-image\",\n          \"quotaResetDelay\": \"99h12m42s\",\n          \"quotaResetTimeStamp\": \"" + AgyPictureReset + "\"\n        }\n      }\n    ]\n  }\n}"
)

// agyPictures plays agy's generate_image in a run whose agent has it, from
// step i on, and returns the next step's place and what the answer says.
func agyPictures(c *Call, agent, conv string, i int) (int, string) {
	if !agyAgentHasTool(c.Dir, agent, "generate_image") {
		return i, ""
	}
	if os.Getenv(EnvPictures) == PicturesLimit {
		agyStep(conv, i, "tool", "ACTIVE", map[string]any{"tool_name": "generate_image"})
		agyStep(conv, i, "tool", "ERROR", map[string]any{"tool_name": "generate_image",
			"tool_info": map[string]any{"name": "generate_image", "error": map[string]any{"message": AgyPictureLimitError}}})
		return i + 1, " (the picture could not be made)"
	}
	brain := os.Getenv(EnvAgyBrain)
	n := picturesWanted()
	if brain == "" || !filepath.IsAbs(brain) || n == 0 {
		return i, ""
	}
	ms := time.Now().UnixMilli()
	first := writePictures(filepath.Join(brain, conv), n, func(k int) string {
		return "icons_cat_" + strconv.Itoa(k+1) + "_" + strconv.FormatInt(ms+int64(k), 10) + ".jpg"
	}, FakeJPEG)
	for range n {
		agyStep(conv, i, "tool", "ACTIVE", map[string]any{"tool_name": "generate_image"})
		agyStep(conv, i, "tool", "DONE", map[string]any{"tool_name": "generate_image"})
		i++
	}
	if first == "" {
		return i, ""
	}
	return i, " [saved: " + first + "]"
}

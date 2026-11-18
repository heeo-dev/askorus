package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/heeo-dev/askorus/agent/internal/config"
)

// realCacheVar and realCache are the user cache folder's variable and its
// value before TestMain moved it.
var realCacheVar, realCache string

// TestMain points the user cache folder (config.CacheBase: the photos'
// uploads folder and the default ask folder) at a temporary folder for
// every test of this package. uninstall's tests opened the person's real
// uploads folder, deleted the photos of the running agent and removed the
// folder, so that agent's uploads failed until it was started again
// (R-129, 2026-11-18).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "askorus-cache-")
	if err != nil {
		panic(err)
	}
	switch runtime.GOOS {
	case "windows":
		realCacheVar = "LOCALAPPDATA"
	case "darwin", "ios":
		realCacheVar = "HOME" // ~/Library/Caches
	default:
		realCacheVar = "XDG_CACHE_HOME"
	}
	realCache = os.Getenv(realCacheVar)
	os.Setenv(realCacheVar, tmp)
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// R-129: what the tests reach as the cache folder is the temporary one.
func TestCacheIsTemporary(t *testing.T) {
	up, err := config.UploadsFolder(filepath.Join(t.TempDir(), "agent"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := os.Getenv(realCacheVar)
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	if !strings.HasPrefix(strings.ToLower(up), strings.ToLower(tmp)+string(filepath.Separator)) {
		t.Errorf("uploads folder %s is not under the tests' cache folder %s (the person's is %s)", up, tmp, realCache)
	}
}

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/update"
)

// keys is a pretend maintainer: passwords typed in turn, a release key
// "locked" by a stand-in for DPAPI (the real one runs in
// TestDPAPIOpensForThisUserOnly), and folders outside any checkout.
type keys struct {
	t                   *testing.T
	rootDir, releaseDir string
	files               string
	typed               []string
}

func newKeys(t *testing.T) *keys {
	t.Helper()
	k := &keys{t: t, rootDir: filepath.Join(t.TempDir(), "usb"), releaseDir: filepath.Join(t.TempDir(), "sign"), files: t.TempDir()}
	saved := []any{now, askPassword, lockReleaseKey, openReleaseKey, rootsBuiltIn, minScryptForUse}
	t.Cleanup(func() {
		now, askPassword = saved[0].(func() time.Time), saved[1].(func(string) ([]byte, error))
		lockReleaseKey, openReleaseKey = saved[2].(func([]byte) ([]byte, error)), saved[3].(func([]byte) ([]byte, error))
		rootsBuiltIn, minScryptForUse = saved[4].(func() ([]update.PublicKey, error)), saved[5].(int)
	})
	now = func() time.Time { return time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC) }
	askPassword = func(string) ([]byte, error) {
		if len(k.typed) == 0 {
			return nil, errors.New("no more passwords typed")
		}
		p := k.typed[0]
		k.typed = k.typed[1:]
		return []byte(p), nil
	}
	lockReleaseKey = func(b []byte) ([]byte, error) { return append([]byte("locked:"), b...), nil }
	openReleaseKey = func(b []byte) ([]byte, error) {
		if !bytes.HasPrefix(b, []byte("locked:")) {
			return nil, errors.New("not locked by this user")
		}
		return bytes.TrimPrefix(b, []byte("locked:")), nil
	}
	minScryptForUse = 1 << 14
	for _, s := range update.Systems {
		agent := make([]byte, 1000)
		rand.Read(agent)
		if err := os.WriteFile(filepath.Join(k.files, update.ProgramName(s[0], s[1])), agent, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return k
}

func (k *keys) run(args ...string) (string, error) {
	var out bytes.Buffer
	err := run(args, &out)
	return out.String(), err
}

func (k *keys) must(args ...string) string {
	k.t.Helper()
	out, err := k.run(args...)
	if err != nil {
		k.t.Fatalf("%v: %v", args, err)
	}
	return out
}

const pw = "correct horse battery"

// The whole way of part 27: root key, release key, certificate, a signed
// release, and a check as an agent makes it.
func TestMakeKeysAndSignARelease(t *testing.T) {
	k := newKeys(t)
	k.typed = []string{pw, pw}
	k.must("root-new", k.rootDir)
	k.must("release-new", k.releaseDir)
	k.typed = []string{pw}
	k.must("certify", k.rootDir, k.releaseDir, "1")
	out := k.must("release", k.releaseDir, k.files, "0.5.0")
	if !strings.Contains(out, "valid until 2027-12-01") {
		t.Fatalf("release said %q", out)
	}
	for _, name := range []string{update.KeyFile, update.KeySigFile, update.ReleaseFile, update.ReleaseSigFile} {
		if _, err := os.Stat(filepath.Join(k.files, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	rootPub := filepath.Join(k.rootDir, rootPubFile)
	if out := k.must("verify", k.files, rootPub); !strings.Contains(out, "0.5.0") || !strings.Contains(out, "5 agents match") {
		t.Fatalf("verify said %q", out)
	}

	// An agent that is not the one signed for.
	win := filepath.Join(k.files, update.ProgramName("windows", "amd64"))
	if err := os.WriteFile(win, []byte("not the agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := k.run("verify", k.files, rootPub); err == nil || !strings.Contains(err.Error(), "is not the file") {
		t.Fatalf("verify a changed agent: %v", err)
	}

	// The agents built with no root key (as before the maintainer makes
	// one) do not take it; with the root key built in they do.
	rootsBuiltIn = func() ([]update.PublicKey, error) { return nil, nil }
	if _, err := k.run("verify", k.files); !errors.Is(err, update.ErrNoRoot) {
		t.Fatalf("verify with no root built in: %v", err)
	}
}

// No secret ever reaches the screen or the public files.
func TestNothingSecretIsPrinted(t *testing.T) {
	k := newKeys(t)
	k.typed = []string{pw, pw}
	out := k.must("root-new", k.rootDir) + k.must("release-new", k.releaseDir)
	k.typed = []string{pw}
	out += k.must("certify", k.rootDir, k.releaseDir, "1") + k.must("release", k.releaseDir, k.files, "0.5.0")
	f, _ := readSecret(filepath.Join(k.releaseDir, releaseKeyFile))
	sk, err := unlockRelease(f, openReleaseKey)
	if err != nil {
		t.Fatal(err)
	}
	seed := sk.Key.Seed()
	public := out
	for _, name := range []string{update.KeyFile, update.ReleaseFile, rootPubFile} {
		dir := k.files
		if name == rootPubFile {
			dir = k.rootDir
		}
		b, _ := os.ReadFile(filepath.Join(dir, name))
		public += string(b)
	}
	for _, secret := range []string{pw, string(seed), hex.EncodeToString(seed), base64.StdEncoding.EncodeToString(seed)} {
		if strings.Contains(public, secret) {
			t.Fatal("a password or a secret key is in the output or a public file")
		}
	}
}

func TestRootKeyNeedsItsPassword(t *testing.T) {
	k := newKeys(t)
	k.typed = []string{pw, pw + "!"}
	if _, err := k.run("root-new", k.rootDir); err == nil || !strings.Contains(err.Error(), "not the same") {
		t.Fatalf("two passwords: %v", err)
	}
	k.typed = []string{"short", "short"}
	if _, err := k.run("root-new", k.rootDir); err == nil || !strings.Contains(err.Error(), "fewer than 12") {
		t.Fatalf("short password: %v", err)
	}
	k.typed = []string{pw, pw}
	k.must("root-new", k.rootDir)
	k.must("release-new", k.releaseDir)
	k.typed = []string{"wrong password!!"}
	if _, err := k.run("certify", k.rootDir, k.releaseDir, "1"); !errors.Is(err, errPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	// A box moved into another key's file does not open either.
	f, _ := readSecret(filepath.Join(k.rootDir, rootKeyFile))
	other, _, _ := update.NewKey()
	f.Pub = other.Base64()
	if _, err := unlockRoot(f, []byte(pw)); !errors.Is(err, errPassword) {
		t.Fatalf("moved box: %v", err)
	}
	// A file that asks for an unbounded cost is refused before any work.
	f, _ = readSecret(filepath.Join(k.rootDir, rootKeyFile))
	f.N = 1 << 30
	if _, err := unlockRoot(f, []byte(pw)); err == nil || !strings.Contains(err.Error(), "unknown lock settings") {
		t.Fatalf("cost 1<<30: %v", err)
	}
}

func TestKeyFilesAreNeverReplaced(t *testing.T) {
	k := newKeys(t)
	k.typed = []string{pw, pw}
	k.must("root-new", k.rootDir)
	k.typed = []string{pw, pw}
	if _, err := k.run("root-new", k.rootDir); err == nil || !strings.Contains(err.Error(), "never replaced") {
		t.Fatalf("second root-new: %v", err)
	}
	k.must("release-new", k.releaseDir)
	if _, err := k.run("release-new", k.releaseDir); err == nil || !strings.Contains(err.Error(), "never replaced") {
		t.Fatalf("second release-new: %v", err)
	}
	// A certificate is public and is replaced, but only by a higher number.
	k.typed = []string{pw}
	k.must("certify", k.rootDir, k.releaseDir, "2")
	if _, err := k.run("certify", k.rootDir, k.releaseDir, "2"); err == nil || !strings.Contains(err.Error(), "higher number") {
		t.Fatalf("same number again: %v", err)
	}
}

func TestKeysStayOutOfRepositories(t *testing.T) {
	k := newKeys(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	k.typed = []string{pw, pw}
	for _, cmd := range []string{"root-new", "release-new"} {
		if _, err := k.run(cmd, filepath.Join(repo, "keys")); err == nil || !strings.Contains(err.Error(), "outside any repository") {
			t.Errorf("%s in a checkout: %v", cmd, err)
		}
	}
}

func TestReleaseRefuses(t *testing.T) {
	k := newKeys(t)
	k.typed = []string{pw, pw}
	k.must("root-new", k.rootDir)
	k.must("release-new", k.releaseDir)
	if _, err := k.run("release", k.releaseDir, k.files, "0.5.0"); err == nil || !strings.Contains(err.Error(), "certify first") {
		t.Fatalf("release before certify: %v", err)
	}
	k.typed = []string{pw}
	k.must("certify", k.rootDir, k.releaseDir, "1")
	if _, err := k.run("release", k.releaseDir, k.files, "0.5.0-dev+abc"); err == nil || !strings.Contains(err.Error(), "not a release version") {
		t.Fatalf("a branch build: %v", err)
	}
	if err := os.Remove(filepath.Join(k.files, update.ProgramName("linux", "arm64"))); err != nil {
		t.Fatal(err)
	}
	if _, err := k.run("release", k.releaseDir, k.files, "0.5.0"); err == nil {
		t.Fatal("released without the Linux ARM agent")
	}
	// The release key of another PC (or another Windows user) does not open.
	openReleaseKey = func([]byte) ([]byte, error) { return nil, errors.New("not this user") }
	if _, err := k.run("release", k.releaseDir, k.files, "0.5.0"); err == nil || !strings.Contains(err.Error(), "this Windows user") {
		t.Fatalf("another user: %v", err)
	}
}

// Off Windows there is no DPAPI, and the release key says so
// (TestDPAPIOpensForThisUserOnly is the Windows side).
func TestNoDPAPIOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has DPAPI")
	}
	if _, err := dpapiLock([]byte("x")); err == nil {
		t.Fatal("DPAPI off Windows")
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"sign"}, {"root-new"}, {"certify", "a", "b", "zero"}, {"certify", "a", "b", "0"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Errorf("%v ran", args)
		}
	}
}

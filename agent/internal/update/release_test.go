package update

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)

const sum = "8f90b0590000000000000000000000000000000000000000000000000000c870"

// testRelease is a release as release.ps1 makes it: a root key, a release
// key it vouches for, and the manifest that key signed.
type testRelease struct {
	t       *testing.T
	root    PublicKey
	rootSec SecretKey
	relSec  SecretKey
	keySeq  int
	m       Manifest
}

func newTestRelease(t *testing.T) *testRelease {
	root, rootSec := mustKey(t)
	_, relSec := mustKey(t)
	return &testRelease{t: t, root: root, rootSec: rootSec, relSec: relSec, keySeq: 1, m: Manifest{
		Kind:      KindRelease,
		Product:   Product,
		Version:   "0.5.1",
		Published: t0.Add(-24 * time.Hour),
		Expires:   t0.Add(365 * 24 * time.Hour),
		Programs: []Program{
			{OS: "windows", Arch: "amd64", Name: "askorus-agent-windows-amd64.exe", Size: 40 << 20, SHA256: sum},
			{OS: "darwin", Arch: "arm64", Name: "askorus-agent-darwin-arm64", Size: 38 << 20, SHA256: sum},
			{OS: "linux", Arch: "amd64", Name: "askorus-agent-linux-amd64", Size: 39 << 20, SHA256: sum},
		},
	}}
}

func (r *testRelease) keyFile() []byte {
	b, _ := json.Marshal(KeyCert{Kind: KindKey, Key: r.relSec.Public().Base64(), Seq: r.keySeq, Issued: t0.Add(-48 * time.Hour)})
	return b
}

func (r *testRelease) releaseFile() []byte {
	b, _ := json.Marshal(r.m)
	return b
}

func (r *testRelease) signed() Signed {
	k, m := r.keyFile(), r.releaseFile()
	return Signed{
		Key:        k,
		KeySig:     mustSign(r.t, r.rootSec, k, KeyTrusted(r.keySeq)),
		Release:    m,
		ReleaseSig: mustSign(r.t, r.relSec, m, ReleaseTrusted(r.m.Version)),
	}
}

func TestCheckTakesASignedRelease(t *testing.T) {
	r := newTestRelease(t)
	r.keySeq = 3
	other, _ := mustKey(t)
	got, err := Check([]PublicKey{other, r.root}, 2, r.signed(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "0.5.1" || got.KeySeq != 3 {
		t.Fatalf("Check = %s, key %d", got.Version, got.KeySeq)
	}
	p, ok := got.For("windows", "amd64")
	if !ok || p.Name != "askorus-agent-windows-amd64.exe" || p.Size != 40<<20 {
		t.Fatalf("For(windows, amd64) = %+v, %v", p, ok)
	}
	if _, ok := got.For("linux", "arm64"); ok {
		t.Fatal("For(linux, arm64) found a program the release does not have")
	}
	// The same key number as the one seen is the same key: taken.
	if _, err := Check([]PublicKey{r.root}, 3, r.signed(), t0); err != nil {
		t.Fatalf("seen key again: %v", err)
	}
}

func TestCheckRefusesWhatIsNotSignedInTheChain(t *testing.T) {
	r := newTestRelease(t)
	good := r.signed()
	stranger, strangerSec := mustKey(t)

	cases := map[string]func() (Signed, []PublicKey){
		// The release key signs its own certificate: only a root may.
		"a release key that vouches for itself": func() (Signed, []PublicKey) {
			s := good
			s.KeySig = mustSign(t, r.relSec, s.Key, KeyTrusted(1))
			return s, []PublicKey{r.root}
		},
		// The root signs the manifest directly: the manifest needs the release key.
		"a manifest signed by the root": func() (Signed, []PublicKey) {
			s := good
			s.ReleaseSig = mustSign(t, r.rootSec, s.Release, ReleaseTrusted("0.5.1"))
			return s, []PublicKey{r.root}
		},
		"a manifest signed by a stranger": func() (Signed, []PublicKey) {
			s := good
			s.ReleaseSig = mustSign(t, strangerSec, s.Release, ReleaseTrusted("0.5.1"))
			return s, []PublicKey{r.root}
		},
		"the two signatures swapped": func() (Signed, []PublicKey) {
			s := good
			s.KeySig, s.ReleaseSig = s.ReleaseSig, s.KeySig
			return s, []PublicKey{r.root}
		},
		"a changed manifest": func() (Signed, []PublicKey) {
			s := good
			s.Release = []byte(strings.Replace(string(s.Release), sum, strings.Repeat("0", 64), 1))
			return s, []PublicKey{r.root}
		},
		"another root": func() (Signed, []PublicKey) {
			return good, []PublicKey{stranger}
		},
	}
	for name, c := range cases {
		s, roots := c()
		if _, err := Check(roots, 0, s, t0); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Check(nil, 0, good, t0); !errors.Is(err, ErrNoRoot) {
		t.Errorf("no root: %v", err)
	}
}

// A leaked release key is replaced by one with a higher number; an agent
// that saw the higher number does not take the old key again.
func TestCheckRefusesAReplacedKey(t *testing.T) {
	r := newTestRelease(t)
	r.keySeq = 1
	if _, err := Check([]PublicKey{r.root}, 2, r.signed(), t0); !errors.Is(err, ErrOldKey) {
		t.Fatalf("old key: %v", err)
	}
}

func TestCheckRefusesAnExpiredRelease(t *testing.T) {
	r := newTestRelease(t)
	for _, now := range []time.Time{r.m.Expires, r.m.Expires.Add(time.Second)} {
		_, err := Check([]PublicKey{r.root}, 0, r.signed(), now)
		if !errors.Is(err, ErrExpired) || !strings.Contains(err.Error(), "clock") {
			t.Errorf("at %s: %v", now, err)
		}
	}
}

// The signed comment names the file and its version, so a signature made
// for one file never passes for another.
func TestCheckRefusesAWrongSignedComment(t *testing.T) {
	r := newTestRelease(t)
	s := r.signed()
	s.ReleaseSig = mustSign(t, r.relSec, s.Release, ReleaseTrusted("0.9.0"))
	if _, err := Check([]PublicKey{r.root}, 0, s, t0); !errors.Is(err, ErrBadFiles) {
		t.Errorf("release comment: %v", err)
	}
	s = r.signed()
	s.KeySig = mustSign(t, r.rootSec, s.Key, KeyTrusted(7))
	if _, err := Check([]PublicKey{r.root}, 0, s, t0); !errors.Is(err, ErrBadFiles) {
		t.Errorf("key comment: %v", err)
	}
	s = r.signed()
	s.KeySig = mustSign(t, r.rootSec, s.Key, ReleaseTrusted("0.5.1"))
	if _, err := Check([]PublicKey{r.root}, 0, s, t0); !errors.Is(err, ErrBadFiles) {
		t.Errorf("a release comment on the key: %v", err)
	}
}

func TestCheckRefusesABadManifest(t *testing.T) {
	win := func(r *testRelease) *Program { return &r.m.Programs[0] }
	cases := map[string]func(r *testRelease){
		"another product":        func(r *testRelease) { r.m.Product = "askorus-app" },
		"another kind":           func(r *testRelease) { r.m.Kind = KindKey },
		"a build of a branch":    func(r *testRelease) { r.m.Version = "0.5.1-dev+abc" },
		"a version of two parts": func(r *testRelease) { r.m.Version = "0.5" },
		"no programs":            func(r *testRelease) { r.m.Programs = nil },
		"expires when published": func(r *testRelease) { r.m.Expires = r.m.Published },
		"a path in the name":     func(r *testRelease) { win(r).Name = "../askorus-agent-windows-amd64.exe" },
		"another file":           func(r *testRelease) { win(r).Name = "install.ps1" },
		"no .exe on Windows":     func(r *testRelease) { win(r).Name = "askorus-agent-windows-amd64" },
		".exe on Linux":          func(r *testRelease) { r.m.Programs[2].Name = "askorus-agent-linux-amd64.exe" },
		"a name of another OS":   func(r *testRelease) { win(r).OS = "linux" },
		"a name of another CPU":  func(r *testRelease) { win(r).Arch = "arm64" },
		"two for one system":     func(r *testRelease) { r.m.Programs = append(r.m.Programs, r.m.Programs[0]) },
		"size 0":                 func(r *testRelease) { win(r).Size = 0 },
		"too large":              func(r *testRelease) { win(r).Size = MaxProgram + 1 },
		"no SHA-256":             func(r *testRelease) { win(r).SHA256 = "" },
		"a short SHA-256":        func(r *testRelease) { win(r).SHA256 = sum[:62] },
		"an upper-case SHA-256":  func(r *testRelease) { win(r).SHA256 = strings.ToUpper(sum) },
	}
	for name, change := range cases {
		r := newTestRelease(t)
		change(r)
		if _, err := Check([]PublicKey{r.root}, 0, r.signed(), t0); !errors.Is(err, ErrBadFiles) {
			t.Errorf("%s: %v", name, err)
		}
	}
	r := newTestRelease(t)
	s := r.signed()
	s.Release = append(s.Release, make([]byte, maxJSON)...)
	if _, err := Check([]PublicKey{r.root}, 0, s, t0); !errors.Is(err, ErrBadFiles) {
		t.Errorf("too large a manifest: %v", err)
	}
}

func TestVersions(t *testing.T) {
	for _, v := range []string{"0.5.0", "1.0.0", "0.10.12"} {
		if !IsRelease(v) {
			t.Errorf("IsRelease(%q) = false", v)
		}
	}
	for _, v := range []string{"", "0.5", "0.5.0.1", "v0.5.0", "0.5.0-dev+abc", "0.05.0", "0.5.-1", "0.5.x", " 0.5.0", "1234567.0.0"} {
		if IsRelease(v) {
			t.Errorf("IsRelease(%q) = true", v)
		}
	}
	newer := [][2]string{{"0.5.1", "0.5.0"}, {"0.10.0", "0.9.9"}, {"1.0.0", "0.99.99"}}
	for _, c := range newer {
		if !Newer(c[0], c[1]) || Newer(c[1], c[0]) {
			t.Errorf("Newer(%s, %s)", c[0], c[1])
		}
	}
	for _, c := range [][2]string{{"0.5.0", "0.5.0"}, {"0.5.1", "0.5.0-dev+abc"}, {"0.5.1-dev", "0.5.0"}, {"0.5.1", ""}} {
		if Newer(c[0], c[1]) {
			t.Errorf("Newer(%q, %q) = true", c[0], c[1])
		}
	}
}

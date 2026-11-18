package update

// A release carries four files besides the programs (Phase 1 plan part 27):
//
//	release-key.json(.minisig)  the release key and its sequence number,
//	                            signed by a root key the agent carries
//	release.json(.minisig)      the version and each program's size and
//	                            SHA-256, signed by that release key
//
// The agent trusts only its root keys. A release key that leaks is
// replaced by one with a higher sequence number; an agent that has seen the
// higher number refuses the lower one from then on. The version is the
// release's sequence: the agent never takes a version that is not newer
// than its own, so an old release cannot be served again as the latest.

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// Product is the program a manifest is for.
	Product = "askorus-agent"

	kindKey     = "askorus-release-key"
	kindRelease = "askorus-release"

	// maxJSON bounds a key or release file read from the network.
	maxJSON = 64 << 10
	// MaxProgram bounds one program's size (0.4.0's largest is about 40 MB).
	MaxProgram = 256 << 20
)

// The names of the release files, as release assets.
const (
	KeyFile        = "release-key.json"
	KeySigFile     = "release-key.json.minisig"
	ReleaseFile    = "release.json"
	ReleaseSigFile = "release.json.minisig"
)

// KeyCert is the release key a root key vouches for.
type KeyCert struct {
	Kind   string    `json:"kind"`
	Key    string    `json:"key"` // PublicKey.Base64
	Seq    int       `json:"seq"` // a higher number replaces a lower one
	Issued time.Time `json:"issued"`
}

// Program is one system's agent in a release.
type Program struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is release.json.
type Manifest struct {
	Kind      string    `json:"kind"`
	Product   string    `json:"product"`
	Version   string    `json:"version"`
	Published time.Time `json:"published"`
	Expires   time.Time `json:"expires"`
	Programs  []Program `json:"programs"`
}

// Signed are the four files as fetched.
type Signed struct {
	Key, KeySig, Release, ReleaseSig []byte
}

// Release is a manifest whose signatures checked out.
type Release struct {
	Manifest
	// KeySeq is the sequence number of the release key that signed it;
	// the caller keeps the highest seen.
	KeySeq int
}

// Errors a person sees the meaning of; each is wrapped with detail.
var (
	ErrNoRoot   = errors.New("this agent has no update key")
	ErrOldKey   = errors.New("the release key was replaced by a newer one")
	ErrExpired  = errors.New("the release information has expired")
	ErrBadFiles = errors.New("the release information is not valid")
)

// Check verifies the chain root key → release key → manifest at time now.
// minKeySeq is the highest release key sequence this agent has seen.
func Check(roots []PublicKey, minKeySeq int, s Signed, now time.Time) (Release, error) {
	if len(roots) == 0 {
		return Release{}, ErrNoRoot
	}
	cert, key, err := checkKey(roots, s.Key, s.KeySig)
	if err != nil {
		return Release{}, err
	}
	if cert.Seq < minKeySeq {
		return Release{}, fmt.Errorf("%w (key %d, seen %d)", ErrOldKey, cert.Seq, minKeySeq)
	}
	m, err := checkManifest(key, s.Release, s.ReleaseSig, now)
	if err != nil {
		return Release{}, err
	}
	return Release{Manifest: m, KeySeq: cert.Seq}, nil
}

func checkKey(roots []PublicKey, data, sig []byte) (KeyCert, PublicKey, error) {
	if len(data) > maxJSON {
		return KeyCert{}, PublicKey{}, fmt.Errorf("%w: %s too large", ErrBadFiles, KeyFile)
	}
	trusted, _, err := Verify(roots, data, sig)
	if err != nil {
		return KeyCert{}, PublicKey{}, fmt.Errorf("%s: %w", KeyFile, err)
	}
	var c KeyCert
	if err := json.Unmarshal(data, &c); err != nil {
		return KeyCert{}, PublicKey{}, fmt.Errorf("%w: %s: %v", ErrBadFiles, KeyFile, err)
	}
	if c.Kind != kindKey || c.Seq < 1 {
		return KeyCert{}, PublicKey{}, fmt.Errorf("%w: %s is not a release key", ErrBadFiles, KeyFile)
	}
	// The signed comment names what was signed, so a signature of one kind
	// of file never passes for another.
	if trusted != KeyTrusted(c.Seq) {
		return KeyCert{}, PublicKey{}, fmt.Errorf("%w: %s: signed comment %q", ErrBadFiles, KeyFile, trusted)
	}
	k, err := ParsePublicKey(c.Key)
	if err != nil {
		return KeyCert{}, PublicKey{}, fmt.Errorf("%w: %s: %v", ErrBadFiles, KeyFile, err)
	}
	return c, k, nil
}

func checkManifest(key PublicKey, data, sig []byte, now time.Time) (Manifest, error) {
	if len(data) > maxJSON {
		return Manifest{}, fmt.Errorf("%w: %s too large", ErrBadFiles, ReleaseFile)
	}
	trusted, _, err := Verify([]PublicKey{key}, data, sig)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", ReleaseFile, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: %v", ErrBadFiles, ReleaseFile, err)
	}
	if err := m.valid(); err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: %v", ErrBadFiles, ReleaseFile, err)
	}
	if trusted != ReleaseTrusted(m.Version) {
		return Manifest{}, fmt.Errorf("%w: %s: signed comment %q", ErrBadFiles, ReleaseFile, trusted)
	}
	if !now.Before(m.Expires) {
		return Manifest{}, fmt.Errorf("%w (on %s; is the PC's clock right?)", ErrExpired, m.Expires.Format(time.DateOnly))
	}
	return m, nil
}

// KeyTrusted and ReleaseTrusted are the signed comments of the two files.
func KeyTrusted(seq int) string            { return fmt.Sprintf("%s %d", kindKey, seq) }
func ReleaseTrusted(version string) string { return kindRelease + " " + version }

var programName = regexp.MustCompile(`^askorus-agent-(windows|darwin|linux)-(amd64|arm64)(\.exe)?$`)

func (m Manifest) valid() error {
	if m.Kind != kindRelease || m.Product != Product {
		return errors.New("not an agent release")
	}
	if !IsRelease(m.Version) {
		return fmt.Errorf("version %q is not a release version", m.Version)
	}
	if !m.Expires.After(m.Published) {
		return errors.New("expires before it was published")
	}
	if len(m.Programs) == 0 {
		return errors.New("no programs")
	}
	seen := map[string]bool{}
	for _, p := range m.Programs {
		g := programName.FindStringSubmatch(p.Name)
		// The name is the asset's name and the system's, nothing else: no
		// folders, no other files, and .exe only on Windows.
		if g == nil || g[1] != p.OS || g[2] != p.Arch || (g[3] == ".exe") != (p.OS == "windows") {
			return fmt.Errorf("program %q does not match %s/%s", p.Name, p.OS, p.Arch)
		}
		if seen[p.OS+"/"+p.Arch] {
			return fmt.Errorf("two programs for %s/%s", p.OS, p.Arch)
		}
		seen[p.OS+"/"+p.Arch] = true
		if p.Size <= 0 || p.Size > MaxProgram {
			return fmt.Errorf("program %s has size %d", p.Name, p.Size)
		}
		if b, err := hex.DecodeString(p.SHA256); err != nil || len(b) != 32 || p.SHA256 != strings.ToLower(p.SHA256) {
			return fmt.Errorf("program %s has no SHA-256", p.Name)
		}
	}
	return nil
}

// For is the program for one system.
func (m Manifest) For(goos, goarch string) (Program, bool) {
	for _, p := range m.Programs {
		if p.OS == goos && p.Arch == goarch {
			return p, true
		}
	}
	return Program{}, false
}

// IsRelease reports whether v is a release version: three numbers and
// nothing else ('0.5.0'; a build of a branch is '0.5.0-dev+abc').
func IsRelease(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

// Newer reports whether release version a is newer than release version
// b. When either is not a release version it is false: a build of a branch
// is never updated (part 27), and nothing replaces a release with it.
func Newer(a, b string) bool {
	va, ok := parseVersion(a)
	if !ok {
		return false
	}
	vb, ok := parseVersion(b)
	if !ok {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" || len(p) > 6 || (len(p) > 1 && p[0] == '0') {
			return out, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strconv.Itoa(n) != p {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

package update

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// github is a pretend GitHub: the release files under /releases, and a
// redirect to its file host for each, as github.com does.
type github struct {
	t       *testing.T
	srv     *httptest.Server
	rel     *testRelease
	program []byte // the agent of every system
	files   map[string][]byte
	hits    atomic.Int32
	headers http.Header // of the last request
}

func newGitHub(t *testing.T) *github {
	g := &github{t: t, rel: newTestRelease(t), program: make([]byte, 4096)}
	rand.Read(g.program)
	sum := sha256.Sum256(g.program)
	for i := range g.rel.m.Programs {
		g.rel.m.Programs[i].Size = int64(len(g.program))
		g.rel.m.Programs[i].SHA256 = hex.EncodeToString(sum[:])
	}
	g.sign()
	g.srv = httptest.NewTLSServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *github) sign() {
	s := g.rel.signed()
	g.files = map[string][]byte{KeyFile: s.Key, KeySigFile: s.KeySig, ReleaseFile: s.Release, ReleaseSigFile: s.ReleaseSig}
}

func (g *github) serve(w http.ResponseWriter, r *http.Request) {
	g.hits.Add(1)
	g.headers = r.Header.Clone()
	switch {
	case strings.HasPrefix(r.URL.Path, "/releases/latest/download/"):
		http.Redirect(w, r, "/assets/"+strings.TrimPrefix(r.URL.Path, "/releases/latest/download/"), http.StatusFound)
	case r.URL.Path == "/releases/download/v"+g.rel.m.Version+"/"+ProgramName("windows", "amd64"):
		w.Write(g.program)
	case strings.HasPrefix(r.URL.Path, "/assets/"):
		b, ok := g.files[strings.TrimPrefix(r.URL.Path, "/assets/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	case strings.HasPrefix(r.URL.Path, "/elsewhere/"):
		http.Redirect(w, r, "https://evil.example/x", http.StatusFound)
	case strings.HasPrefix(r.URL.Path, "/plain/"):
		http.Redirect(w, r, "http://"+r.Host+"/assets/x", http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

func (g *github) source() Source {
	u, _ := url.Parse(g.srv.URL)
	return Source{Base: g.srv.URL + "/releases", Hosts: []string{u.Hostname()}, Agent: "askorus-agent/0.5.0", Transport: g.srv.Client().Transport}
}

func (g *github) checker(dir, current string, now time.Time) *Checker {
	return &Checker{Dir: dir, Current: current, GOOS: "windows", GOARCH: "amd64", Source: g.source(),
		Roots: func() ([]PublicKey, error) { return []PublicKey{g.rel.root}, nil }, Now: func() time.Time { return now }}
}

func TestLatestFetchesTheFourFiles(t *testing.T) {
	g := newGitHub(t)
	s, err := g.source().Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(s.Release) != string(g.files[ReleaseFile]) || string(s.KeySig) != string(g.files[KeySigFile]) {
		t.Fatal("Latest did not return the files served")
	}
	// What the request carries: the agent's name, nothing of the person.
	if ua := g.headers.Get("User-Agent"); ua != "askorus-agent/0.5.0" {
		t.Fatalf("User-Agent %q", ua)
	}
	for k := range g.headers {
		if k != "User-Agent" && k != "Accept-Encoding" {
			t.Errorf("the request carries %s", k)
		}
	}
	// A release made before #35 (or with -NoSign) has no release.json.
	delete(g.files, ReleaseFile)
	if _, err := g.source().Latest(context.Background()); !errors.Is(err, ErrNoReleaseInfo) {
		t.Fatalf("no release.json: %v", err)
	}
	g.files[ReleaseFile] = make([]byte, maxJSON+1)
	if _, err := g.source().Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("too large: %v", err)
	}
}

func TestFetchGoesOnlyToGitHubOverHTTPS(t *testing.T) {
	g := newGitHub(t)
	ctx := context.Background()
	s := g.source()
	for _, path := range []string{"/elsewhere/x", "/plain/x"} {
		if _, _, err := s.get(ctx, g.srv.URL+path, 100); err == nil || !strings.Contains(err.Error(), "may not be fetched") {
			t.Errorf("redirect %s: %v", path, err)
		}
	}
	// The base itself must be one of the hosts, over https.
	other := s
	other.Hosts = []string{"github.com"}
	if _, err := other.Latest(ctx); err == nil || !strings.Contains(err.Error(), "may not be fetched") {
		t.Errorf("a host not on the list: %v", err)
	}
	other = s
	other.Base = strings.Replace(g.srv.URL, "https://", "http://", 1) + "/releases"
	if _, err := other.Latest(ctx); err == nil || !strings.Contains(err.Error(), "may not be fetched") {
		t.Errorf("http: %v", err)
	}
	if !slicesEqual(DefaultHosts, []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}) || NewSource("1.0.0").Base != "https://github.com/heeo-dev/askorus/releases" {
		t.Error("the default source changed; it is part of the privacy policy and part 27")
	}
}

func slicesEqual(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func TestDownloadChecksSizeAndSHA256(t *testing.T) {
	g := newGitHub(t)
	ctx := context.Background()
	p, _ := g.rel.m.For("windows", "amd64")
	dst := filepath.Join(t.TempDir(), "askorus-agent.new.exe")
	if err := g.source().Download(ctx, "0.5.1", p, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(g.program) {
		t.Fatal("the downloaded file is not the program")
	}
	if err := g.source().Download(ctx, "0.5.1", p, dst); err == nil {
		t.Fatal("a download wrote over a file that was there")
	}

	wrong := p
	wrong.SHA256 = strings.Repeat("0", 64)
	dst2 := filepath.Join(t.TempDir(), "x.exe")
	if err := g.source().Download(ctx, "0.5.1", wrong, dst2); !errors.Is(err, ErrWrongFile) {
		t.Fatalf("wrong SHA-256: %v", err)
	}
	if _, err := os.Stat(dst2); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a wrong download was left behind")
	}
	short := p
	short.Size--
	if err := g.source().Download(ctx, "0.5.1", short, dst2); !errors.Is(err, ErrWrongFile) {
		t.Fatalf("longer than named: %v", err)
	}
	for _, c := range []struct{ v, name string }{{"0.5.1-dev", p.Name}, {"0.5.1", "../evil.exe"}, {"0.5.1", "install.ps1"}} {
		q := p
		q.Name = c.name
		if err := g.source().Download(ctx, c.v, q, dst2); !errors.Is(err, ErrBadFiles) {
			t.Errorf("%s %s: %v", c.v, c.name, err)
		}
	}
}

func TestCheckerRemembersSixHours(t *testing.T) {
	g := newGitHub(t)
	dir := t.TempDir()
	ctx := context.Background()

	f, err := g.checker(dir, "0.5.0", t0).Check(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Newer || f.Latest != "0.5.1" || f.Release == nil || f.Program.Name != "askorus-agent-windows-amd64.exe" {
		t.Fatalf("first check: %+v", f)
	}
	if st := LoadState(dir); st.KeySeq != 1 || st.Latest != "0.5.1" || !st.Checked.Equal(t0) {
		t.Fatalf("state: %+v", st)
	}
	hits := g.hits.Load()

	// Within six hours the phone's question is answered from memory.
	f, err = g.checker(dir, "0.5.0", t0.Add(5*time.Hour)).Check(ctx, false)
	if err != nil || !f.Newer || f.Latest != "0.5.1" || f.Release != nil || g.hits.Load() != hits {
		t.Fatalf("remembered check: %+v, %v, %d requests", f, err, g.hits.Load()-hits)
	}
	// Asked fresh (to take the update), or later, or with the clock set
	// back, it asks again (each after a check at t0).
	for _, c := range []struct {
		at    time.Time
		fresh bool
	}{{t0.Add(5 * time.Hour), true}, {t0.Add(6 * time.Hour), false}, {t0.Add(-time.Hour), false}} {
		dir := t.TempDir()
		if _, err := g.checker(dir, "0.5.0", t0).Check(ctx, false); err != nil {
			t.Fatal(err)
		}
		before := g.hits.Load()
		if _, err := g.checker(dir, "0.5.0", c.at).Check(ctx, c.fresh); err != nil {
			t.Fatal(err)
		}
		if g.hits.Load() == before {
			t.Errorf("at %s fresh=%v: answered from memory", c.at, c.fresh)
		}
	}
}

func TestCheckerKeepsTheHighestKeyNumber(t *testing.T) {
	g := newGitHub(t)
	dir := t.TempDir()
	ctx := context.Background()
	g.rel.keySeq = 2
	g.sign()
	if _, err := g.checker(dir, "0.5.0", t0).Check(ctx, true); err != nil {
		t.Fatal(err)
	}
	// Someone serves the release signed under the replaced key 1.
	g.rel.keySeq = 1
	g.sign()
	if _, err := g.checker(dir, "0.5.0", t0).Check(ctx, true); !errors.Is(err, ErrOldKey) {
		t.Fatalf("replaced key: %v", err)
	}
}

func TestCheckerFindsNothingNewer(t *testing.T) {
	g := newGitHub(t)
	ctx := context.Background()
	for _, current := range []string{"0.5.1", "0.6.0", "0.5.1-dev+abc"} {
		f, err := g.checker(t.TempDir(), current, t0).Check(ctx, true)
		if err != nil || f.Newer || f.Release != nil {
			t.Errorf("current %s: %+v, %v", current, f, err)
		}
	}
	c := g.checker(t.TempDir(), "0.5.0", t0)
	c.GOOS, c.GOARCH = "linux", "arm64"
	if _, err := c.Check(ctx, true); !errors.Is(err, ErrBadFiles) {
		t.Errorf("no agent for linux/arm64: %v", err)
	}
	c = g.checker(t.TempDir(), "0.5.0", t0)
	c.Roots = func() ([]PublicKey, error) { return nil, nil }
	before := g.hits.Load()
	if _, err := c.Check(ctx, true); !errors.Is(err, ErrNoRoot) {
		t.Errorf("no root built in: %v", err)
	}
	if g.hits.Load() != before {
		t.Error("an agent without a root key asked GitHub")
	}
}

func TestInstallPath(t *testing.T) {
	env := func(k string) string {
		if k == "LOCALAPPDATA" {
			return `C:\Users\someone\AppData\Local`
		}
		return ""
	}
	if got := InstallPath("windows", env, ""); got != filepath.Join(`C:\Users\someone\AppData\Local`, "Programs", "Askorus", "askorus-agent.exe") {
		t.Errorf("windows: %s", got)
	}
	if got := InstallPath("linux", env, "/home/someone"); got != filepath.Join("/home/someone", ".local", "bin", "askorus-agent") {
		t.Errorf("linux: %s", got)
	}
	if InstallPath("windows", func(string) string { return "" }, "") != "" || InstallPath("darwin", env, "") != "" {
		t.Error("an install path without its folder")
	}
	if !SamePath("windows", `C:\A\b.exe`, `c:\a\B.EXE`) || SamePath("linux", "/a/b", "/a/B") || SamePath("linux", "", "") {
		t.Error("SamePath")
	}
}

func TestStageSwapRestore(t *testing.T) {
	g := newGitHub(t)
	ctx := context.Background()
	dir := t.TempDir()
	exe := filepath.Join(dir, "askorus-agent.exe")
	if err := os.WriteFile(exe, []byte("old agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := g.checker(t.TempDir(), "0.5.0", t0).Check(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	saved := Says
	t.Cleanup(func() { Says = saved })

	// The new program must say it is the version the release names.
	Says = func(string) (string, error) { return "askorus-agent 0.5.0", nil }
	if _, err := Stage(ctx, g.source(), f, "windows", exe); err == nil || !strings.Contains(err.Error(), "says") {
		t.Fatalf("a program that says another version: %v", err)
	}
	if _, err := os.Stat(Sibling(exe, "windows", "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the refused program was left behind")
	}
	Says = func(string) (string, error) { return "askorus-agent 0.5.1", nil }
	// A .new left by an earlier try is replaced.
	os.WriteFile(Sibling(exe, "windows", "new"), []byte("stale"), 0o755)
	staged, err := Stage(ctx, g.source(), f, "windows", exe)
	if err != nil || staged != filepath.Join(dir, "askorus-agent.new.exe") {
		t.Fatalf("Stage = %s, %v", staged, err)
	}

	// A prev from an earlier update is replaced by this one's.
	os.WriteFile(Sibling(exe, "windows", "prev"), []byte("older agent"), 0o755)
	prev, err := Swap(exe, staged, "windows")
	if err != nil || prev != filepath.Join(dir, "askorus-agent.prev.exe") {
		t.Fatalf("Swap = %s, %v", prev, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(g.program) {
		t.Fatal("the new program is not in place")
	}
	if b, _ := os.ReadFile(prev); string(b) != "old agent" {
		t.Fatal("the old program was not kept")
	}

	if err := Restore(exe, prev, "windows"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old agent" {
		t.Fatal("the old program is not back")
	}
	if b, _ := os.ReadFile(Sibling(exe, "windows", "failed")); string(b) != string(g.program) {
		t.Fatal("the program that failed was not kept for a look")
	}

	if _, err := Stage(ctx, g.source(), Found{}, "windows", exe); err == nil {
		t.Fatal("staged with nothing newer")
	}
	if got := Sibling("/home/x/.local/bin/askorus-agent", "linux", "prev"); got != filepath.Join("/home/x/.local/bin", "askorus-agent.prev") {
		t.Errorf("unix prev: %s", got)
	}
}

// A program a scanner holds for a moment is renamed once it lets go; one
// that is held longer fails, and the old program stays in place.
func TestSwapWaitsForAHeldFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("holding a file against a rename is a Windows thing")
	}
	saved := renameWait
	renameWait = 2 * time.Second
	t.Cleanup(func() { renameWait = saved })
	dir := t.TempDir()
	exe := filepath.Join(dir, "askorus-agent.exe")
	staged := filepath.Join(dir, "askorus-agent.new.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(staged, []byte("new"), 0o755)
	held, err := os.Open(staged) // Go opens without FILE_SHARE_DELETE: a rename fails while it is open
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(600 * time.Millisecond); held.Close() }()
	if _, err := Swap(exe, staged, "windows"); err != nil {
		t.Fatalf("Swap after the file was let go: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("the program is %q", b)
	}

	renameWait = 300 * time.Millisecond
	os.WriteFile(staged, []byte("newer"), 0o755)
	held, _ = os.Open(staged)
	defer held.Close()
	if _, err := Swap(exe, staged, "windows"); err == nil {
		t.Fatal("swapped a file that stayed held")
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("after a failed swap the program is %q", b)
	}
}

func TestProgress(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadProgress(dir); ok {
		t.Fatal("progress before any update")
	}
	p := Progress{From: "0.5.0", To: "0.5.1", State: ProgressRolledBack, Reason: "the new agent did not start", At: t0}
	if err := WriteProgress(dir, p); err != nil {
		t.Fatal(err)
	}
	if got, ok := ReadProgress(dir); !ok || got != p {
		t.Fatalf("ReadProgress = %+v, %v", got, ok)
	}
}

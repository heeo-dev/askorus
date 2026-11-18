package update

// Fetching from the one place releases come from (Phase 1 plan part 27):
// the release files of github.com/heeo-dev/askorus, by the
// releases/latest/download/ and releases/download/v<version>/ addresses
// (no GitHub API, so no request limit). GitHub answers them with a
// redirect to its file host; only https and only these hosts are followed.
// Nothing else is sent: no device token, conversation, folder or PC name,
// only what any download carries (the address, and an agent name with its
// version).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"
)

// DefaultBase is where releases are fetched from.
const DefaultBase = "https://github.com/heeo-dev/askorus/releases"

// DefaultHosts are the hosts a fetch may be sent to: GitHub, and the
// hosts its release downloads redirect to.
var DefaultHosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

// Source fetches release files. The zero value is not usable; see
// NewSource.
type Source struct {
	Base  string   // DefaultBase; tests point it at their own server
	Hosts []string // hosts that may be asked, the Base's among them
	Agent string   // the User-Agent: "askorus-agent/<version>"
	// Transport carries the requests (nil: Go's default). Every request
	// goes through a client this package makes, with its redirect rule.
	Transport http.RoundTripper
}

// ErrNoReleaseInfo: the latest release carries no signed release
// information (one made before #35, or made with -NoSign).
var ErrNoReleaseInfo = errors.New("the latest release has no signed update information")

// NewSource is the source agents use. version goes in the User-Agent.
func NewSource(version string) Source {
	return Source{Base: DefaultBase, Hosts: DefaultHosts, Agent: "askorus-agent/" + version}
}

// client follows redirects only over https and only to s.Hosts, at most
// five times, with a timeout for the whole request (0: the context's).
func (s Source) client(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: s.Transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			// Go names the page it comes from; nothing but the address goes.
			req.Header.Del("Referer")
			return s.allowed(req.URL)
		},
	}
}

func (s Source) allowed(u *url.URL) error {
	if u.Scheme != "https" || !slices.Contains(s.Hosts, u.Hostname()) || u.User != nil {
		return fmt.Errorf("an update may not be fetched from %s://%s", u.Scheme, u.Host)
	}
	return nil
}

// get fetches one file into memory, at most max bytes, in 30 seconds.
func (s Source) get(ctx context.Context, rawURL string, max int64) ([]byte, int, error) {
	resp, err := s.open(ctx, s.client(30*time.Second), rawURL)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(b)) > max {
		return nil, resp.StatusCode, fmt.Errorf("%s: larger than %d bytes", rawURL, max)
	}
	return b, resp.StatusCode, nil
}

func (s Source) open(ctx context.Context, c *http.Client, rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := s.allowed(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.Agent)
	return c.Do(req)
}

// Latest fetches the four signed files of the latest release. It does not
// check them; Check does.
func (s Source) Latest(ctx context.Context) (Signed, error) {
	var out Signed
	for _, f := range []struct {
		name string
		dst  *[]byte
		max  int64
	}{
		{KeyFile, &out.Key, maxJSON},
		{KeySigFile, &out.KeySig, maxSigFile},
		{ReleaseFile, &out.Release, maxJSON},
		{ReleaseSigFile, &out.ReleaseSig, maxSigFile},
	} {
		b, status, err := s.get(ctx, s.Base+"/latest/download/"+f.name, f.max)
		if status == http.StatusNotFound {
			return Signed{}, ErrNoReleaseInfo
		}
		if err != nil {
			return Signed{}, err
		}
		*f.dst = b
	}
	return out, nil
}

// ErrWrongFile: what was downloaded is not the program the signed
// release information names.
var ErrWrongFile = errors.New("the downloaded agent is not the one the release names")

// Download fetches release version's program p into a new file at dst
// and checks its size and SHA-256 against the signed information. On any
// failure dst is removed. The download gets 10 minutes.
func (s Source) Download(ctx context.Context, version string, p Program, dst string) (err error) {
	if !IsRelease(version) || !programName.MatchString(p.Name) {
		return fmt.Errorf("%w: %s %s", ErrBadFiles, version, p.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	// The context bounds it; 30 seconds would cut a slow line.
	resp, err := s.open(ctx, s.client(0), s.Base+"/download/v"+version+"/"+p.Name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", p.Name, resp.Status)
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(dst)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, p.Size+1))
	if err != nil {
		return err
	}
	if n != p.Size || hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		return fmt.Errorf("%w (%d bytes)", ErrWrongFile, n)
	}
	return f.Sync()
}

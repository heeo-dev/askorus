// askorus-sign makes and uses the keys that sign agent updates (#35,
// Phase 1 plan part 27). It is the maintainer's tool, never part of a
// release:
//
//	askorus-sign root-new <folder>
//	    make the root key in <folder> (a USB drive, off the PC); asks for a
//	    password twice in the terminal
//	askorus-sign release-new <folder>
//	    make the release key in <folder>, locked to this Windows user
//	askorus-sign certify <root folder> <release folder> <number>
//	    the root key vouches for the release key; writes release-key.json
//	    and its signature into the release folder
//	askorus-sign release <release folder> <files folder> <version>
//	    write release.json for the agents in <files folder>, sign it, and
//	    copy release-key.json there: the four files a release carries
//	askorus-sign verify <files folder> [root.pub ...]
//	    check the four files as an agent does (with the root keys it is
//	    built with, or the given ones), and each agent's size and SHA-256
//
// Secret keys and passwords are never printed. Key folders may not be in a
// git checkout, and key files are never replaced.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/update"

	"golang.org/x/term"
)

const (
	rootKeyFile    = "root.key"
	rootPubFile    = "root.pub"
	releaseKeyFile = "release.key"
	releasePubFile = "release.pub"

	// validFor is how long release information is taken (part 27).
	validFor = 365 * 24 * time.Hour
)

// Seams for tests.
var (
	now             = time.Now
	askPassword     = terminalPassword
	lockReleaseKey  = dpapiLock
	openReleaseKey  = dpapiUnlock
	rootsBuiltIn    = update.Roots
	minScryptForUse = scryptN // tests make the root key cheaper
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "askorus-sign:", err)
		os.Exit(1)
	}
}

const usage = `usage:
  askorus-sign root-new <folder>
  askorus-sign release-new <folder>
  askorus-sign certify <root folder> <release folder> <number>
  askorus-sign release <release folder> <files folder> <version>
  askorus-sign verify <files folder> [root.pub ...]`

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cmd, rest := args[0], args[1:]
	switch {
	case cmd == "root-new" && len(rest) == 1:
		return rootNew(rest[0], out)
	case cmd == "release-new" && len(rest) == 1:
		return releaseNew(rest[0], out)
	case cmd == "certify" && len(rest) == 3:
		seq, err := strconv.Atoi(rest[2])
		if err != nil || seq < 1 {
			return errors.New("the number is 1 for the first release key, and one more for each new one")
		}
		return certify(rest[0], rest[1], seq, out)
	case cmd == "release" && len(rest) == 3:
		return release(rest[0], rest[1], rest[2], out)
	case cmd == "verify" && len(rest) >= 1:
		return verify(rest[0], rest[1:], out)
	}
	return errors.New(usage)
}

func rootNew(dir string, out io.Writer) error {
	if err := keyFolder(dir); err != nil {
		return err
	}
	pw, err := askPassword("password for the root key (at least 12 characters): ")
	if err != nil {
		return err
	}
	again, err := askPassword("the same password again: ")
	if err != nil {
		return err
	}
	if string(pw) != string(again) {
		return errors.New("the two passwords are not the same; nothing was made")
	}
	if len([]rune(string(pw))) < minPassword {
		return fmt.Errorf("the password has fewer than %d characters; nothing was made", minPassword)
	}
	pub, sec, err := update.NewKey()
	if err != nil {
		return err
	}
	f, err := lockRoot(sec, pw, minScryptForUse)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, rootKeyFile), f); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(dir, rootPubFile), []byte(pub.File())); err != nil {
		return err
	}
	fmt.Fprintf(out, "made the root key %s in %s\n", pub.ID, dir)
	fmt.Fprintf(out, "  %s  locked with your password: keep it off the PC, with a copy\n", rootKeyFile)
	fmt.Fprintf(out, "  %s  the public key: copy it to agent/internal/update/roots/ in the repo\n", rootPubFile)
	return nil
}

func releaseNew(dir string, out io.Writer) error {
	if err := keyFolder(dir); err != nil {
		return err
	}
	pub, sec, err := update.NewKey()
	if err != nil {
		return err
	}
	f, err := lockRelease(sec, lockReleaseKey)
	if err != nil {
		return err
	}
	// It must open again before it is kept.
	if _, err := unlockRelease(f, openReleaseKey); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, releaseKeyFile), f); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(dir, releasePubFile), []byte(pub.File())); err != nil {
		return err
	}
	fmt.Fprintf(out, "made the release key %s in %s, locked to this Windows user\n", pub.ID, dir)
	fmt.Fprintln(out, "  next: askorus-sign certify <root folder> "+dir+" 1")
	return nil
}

func certify(rootDir, releaseDir string, seq int, out io.Writer) error {
	f, err := readSecret(filepath.Join(rootDir, rootKeyFile))
	if err != nil {
		return err
	}
	relPub, err := readPub(filepath.Join(releaseDir, releasePubFile))
	if err != nil {
		return err
	}
	// A new certificate replaces the old one only with a higher number:
	// agents that saw the higher number refuse the lower one.
	if old, err := os.ReadFile(filepath.Join(releaseDir, update.KeyFile)); err == nil {
		var c update.KeyCert
		if json.Unmarshal(old, &c) == nil && c.Seq >= seq {
			return fmt.Errorf("%s already has number %d; give a higher number", update.KeyFile, c.Seq)
		}
	}
	pw, err := askPassword("password of the root key: ")
	if err != nil {
		return err
	}
	root, err := unlockRoot(f, pw)
	if err != nil {
		return err
	}
	cert, err := json.MarshalIndent(update.KeyCert{Kind: update.KindKey, Key: relPub.Base64(), Seq: seq, Issued: now().UTC().Truncate(time.Second)}, "", "  ")
	if err != nil {
		return err
	}
	cert = append(cert, '\n')
	sig, err := update.Sign(root, cert, update.KeyTrusted(seq))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(releaseDir, update.KeyFile), cert, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(releaseDir, update.KeySigFile), sig, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "the root key %s vouches for the release key %s as number %d\n", root.ID, relPub.ID, seq)
	return nil
}

func release(releaseDir, filesDir, version string, out io.Writer) error {
	if !update.IsRelease(version) {
		return fmt.Errorf("%q is not a release version (three numbers, such as 0.5.0)", version)
	}
	f, err := readSecret(filepath.Join(releaseDir, releaseKeyFile))
	if err != nil {
		return err
	}
	sk, err := unlockRelease(f, openReleaseKey)
	if err != nil {
		return err
	}
	cert, err := os.ReadFile(filepath.Join(releaseDir, update.KeyFile))
	if err != nil {
		return fmt.Errorf("%w (run askorus-sign certify first)", err)
	}
	certSig, err := os.ReadFile(filepath.Join(releaseDir, update.KeySigFile))
	if err != nil {
		return err
	}
	var c update.KeyCert
	if err := json.Unmarshal(cert, &c); err != nil {
		return fmt.Errorf("%s: %v", update.KeyFile, err)
	}
	if pub := sk.Public(); c.Key != pub.Base64() {
		return fmt.Errorf("%s vouches for another key, not the release key %s", update.KeyFile, pub.ID)
	}
	t := now().UTC().Truncate(time.Second)
	m := update.Manifest{Kind: update.KindRelease, Product: update.Product, Version: version, Published: t, Expires: t.Add(validFor)}
	for _, s := range update.Systems {
		p, err := program(filesDir, s[0], s[1])
		if err != nil {
			return err
		}
		m.Programs = append(m.Programs, p)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	sig, err := update.Sign(sk, data, update.ReleaseTrusted(version))
	if err != nil {
		return err
	}
	for name, b := range map[string][]byte{update.ReleaseFile: data, update.ReleaseSigFile: sig, update.KeyFile: cert, update.KeySigFile: certSig} {
		if err := os.WriteFile(filepath.Join(filesDir, name), b, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "signed %s %s with the release key %s (number %d), valid until %s\n", update.ReleaseFile, version, sk.ID, c.Seq, m.Expires.Format(time.DateOnly))
	return nil
}

func program(dir, goos, goarch string) (update.Program, error) {
	name := update.ProgramName(goos, goarch)
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return update.Program{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return update.Program{}, err
	}
	return update.Program{OS: goos, Arch: goarch, Name: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func verify(filesDir string, rootFiles []string, out io.Writer) error {
	var roots []update.PublicKey
	if len(rootFiles) == 0 {
		var err error
		if roots, err = rootsBuiltIn(); err != nil {
			return err
		}
	}
	for _, p := range rootFiles {
		k, err := readPub(p)
		if err != nil {
			return err
		}
		roots = append(roots, k)
	}
	var s update.Signed
	for name, dst := range map[string]*[]byte{update.KeyFile: &s.Key, update.KeySigFile: &s.KeySig, update.ReleaseFile: &s.Release, update.ReleaseSigFile: &s.ReleaseSig} {
		b, err := os.ReadFile(filepath.Join(filesDir, name))
		if err != nil {
			return err
		}
		*dst = b
	}
	r, err := update.Check(roots, 0, s, now())
	if err != nil {
		return err
	}
	for _, p := range r.Programs {
		got, err := program(filesDir, p.OS, p.Arch)
		if err != nil {
			return err
		}
		if got.Size != p.Size || got.SHA256 != p.SHA256 {
			return fmt.Errorf("%s is not the file %s names", p.Name, update.ReleaseFile)
		}
	}
	fmt.Fprintf(out, "%s %s: signed by release key number %d, vouched for by a root key; %d agents match\n", update.ReleaseFile, r.Version, r.KeySeq, len(r.Programs))
	return nil
}

func readPub(path string) (update.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return update.PublicKey{}, err
	}
	k, err := update.ParsePublicKey(string(b))
	if err != nil {
		return update.PublicKey{}, fmt.Errorf("%s: %w", path, err)
	}
	return k, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeNew(path, append(b, '\n'))
}

// keyFolder makes dir if needed and refuses a folder inside a git checkout,
// so a secret key never ends up in a repository.
func keyFolder(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for d := abs; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return fmt.Errorf("%s is inside the git checkout %s; keep keys outside any repository", abs, d)
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return os.MkdirAll(abs, 0o700)
}

// terminalPassword reads a password from the terminal without showing it.
// It refuses when the input is not a terminal: the password is typed by
// its owner, never piped in by a program.
func terminalPassword(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("type the password in a terminal window you opened yourself")
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	return pw, err
}

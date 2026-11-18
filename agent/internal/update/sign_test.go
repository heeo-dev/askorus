package update

import (
	"bytes"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"testing"
)

func mustKey(t *testing.T) (PublicKey, SecretKey) {
	t.Helper()
	pub, sec, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return pub, sec
}

func mustSign(t *testing.T, sk SecretKey, msg []byte, trusted string) []byte {
	t.Helper()
	sig, err := Sign(sk, msg, trusted)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func TestSignAndVerify(t *testing.T) {
	pub, sec := mustKey(t)
	other, _ := mustKey(t)
	msg := []byte("the release\n")
	sig := mustSign(t, sec, msg, "askorus-release 0.5.0")

	trusted, by, err := Verify([]PublicKey{other, pub}, msg, sig)
	if err != nil || trusted != "askorus-release 0.5.0" || by.ID != pub.ID {
		t.Fatalf("Verify = %q, %v, %v", trusted, by.ID, err)
	}
	if sec.Public().Base64() != pub.Base64() {
		t.Fatal("Public() is not the key's public key")
	}
	// The file as minisign writes it: four lines, ED, the key's id.
	lines := strings.Split(strings.TrimSuffix(string(sig), "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "untrusted comment: ") || lines[2] != "trusted comment: askorus-release 0.5.0" {
		t.Fatalf("signature file:\n%s", sig)
	}
	raw, _ := base64.StdEncoding.DecodeString(lines[1])
	if string(raw[:2]) != "ED" || !bytes.Equal(raw[2:10], pub.ID[:]) {
		t.Fatalf("signature line starts %q", raw[:10])
	}
	// Windows line ends are read too.
	if _, _, err := Verify([]PublicKey{pub}, msg, bytes.ReplaceAll(sig, []byte("\n"), []byte("\r\n"))); err != nil {
		t.Fatalf("CRLF: %v", err)
	}
}

func TestVerifyRefuses(t *testing.T) {
	pub, sec := mustKey(t)
	other, otherSec := mustKey(t)
	msg := []byte("the release")
	sig := mustSign(t, sec, msg, "askorus-release 0.5.0")
	lines := strings.Split(string(sig), "\n")

	cases := map[string]struct {
		keys []PublicKey
		msg  []byte
		sig  []byte
		bad  bool // ErrBadSignature, not just an unreadable file
	}{
		"a changed file":             {[]PublicKey{pub}, []byte("the release!"), sig, true},
		"a changed trusted comment":  {[]PublicKey{pub}, msg, []byte(strings.Replace(string(sig), "0.5.0", "9.9.9", 1)), true},
		"a key that is not trusted":  {[]PublicKey{other}, msg, sig, true},
		"no keys":                    {nil, msg, sig, true},
		"another key with the id":    {[]PublicKey{{ID: pub.ID, Key: other.Key}}, msg, sig, true},
		"another key's signature":    {[]PublicKey{pub}, msg, signedAs(t, otherSec, pub.ID, msg), true},
		"an empty file":              {[]PublicKey{pub}, msg, nil, false},
		"three lines":                {[]PublicKey{pub}, msg, []byte(strings.Join(lines[:3], "\n")), false},
		"a line after the signature": {[]PublicKey{pub}, msg, append(bytes.Clone(sig), "more\n"...), false},
		"no untrusted comment":       {[]PublicKey{pub}, msg, []byte(strings.Join(append([]string{"x"}, lines[1:]...), "\n")), false},
		"too large":                  {[]PublicKey{pub}, msg, bytes.Repeat([]byte("a"), maxSigFile+1), false},
	}
	for name, c := range cases {
		_, _, err := Verify(c.keys, c.msg, c.sig)
		if err == nil {
			t.Errorf("%s: passed", name)
			continue
		}
		if c.bad != errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// signedAs signs msg with sk but writes id into the signature, as a key
// claiming to be another would.
func signedAs(t *testing.T, sk SecretKey, id KeyID, msg []byte) []byte {
	t.Helper()
	sk.ID = id
	return mustSign(t, sk, msg, "askorus-release 0.5.0")
}

// minisign's older "Ed" signs the whole file instead of its hash; one rule
// for every file, so it is refused even when the signature is right.
func TestVerifyRefusesTheOldAlgorithm(t *testing.T) {
	pub, sec := mustKey(t)
	sig := mustSign(t, sec, []byte("x"), "c")
	lines := strings.Split(string(sig), "\n")
	raw, _ := base64.StdEncoding.DecodeString(lines[1])
	copy(raw[:2], "Ed")
	lines[1] = base64.StdEncoding.EncodeToString(raw)
	_, _, err := Verify([]PublicKey{pub}, []byte("x"), []byte(strings.Join(lines, "\n")))
	if err == nil || !strings.Contains(err.Error(), "not taken") {
		t.Fatalf("Ed: %v", err)
	}
}

func TestSignRefusesATrustedCommentOfTwoLines(t *testing.T) {
	_, sec := mustKey(t)
	for _, c := range []string{"a\nb", "a\rb"} {
		if _, err := Sign(sec, []byte("x"), c); err == nil {
			t.Errorf("%q: signed", c)
		}
	}
}

// Every key the agent is built with reads; with none yet, the list is empty.
func TestEmbeddedRootsRead(t *testing.T) {
	keys, err := Roots()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[KeyID]bool{}
	for _, k := range keys {
		if seen[k.ID] {
			t.Fatalf("two root keys with id %s", k.ID)
		}
		seen[k.ID] = true
	}
}

func TestPublicKeyText(t *testing.T) {
	pub, _ := mustKey(t)
	if !regexp.MustCompile(`^[0-9A-F]{16}$`).MatchString(pub.ID.String()) {
		t.Fatalf("id %q", pub.ID.String())
	}
	if !strings.HasPrefix(pub.Base64(), "RW") {
		t.Fatalf("key line %q does not start as minisign's (RW…)", pub.Base64())
	}
	for _, s := range []string{pub.Base64(), pub.File(), " " + pub.Base64() + "\n", strings.ReplaceAll(pub.File(), "\n", "\r\n")} {
		k, err := ParsePublicKey(s)
		if err != nil || k.ID != pub.ID || !bytes.Equal(k.Key, pub.Key) {
			t.Errorf("ParsePublicKey(%q) = %v", s, err)
		}
	}
	raw, _ := base64.StdEncoding.DecodeString(pub.Base64())
	copy(raw[:2], "ED")
	for _, s := range []string{"", "RW", "untrusted comment: x", base64.StdEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw[:20])} {
		if _, err := ParsePublicKey(s); err == nil {
			t.Errorf("ParsePublicKey(%q) passed", s)
		}
	}
}

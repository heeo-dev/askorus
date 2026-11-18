// Package update checks and applies agent updates (#35, Phase 1 plan part
// 27). This file holds the signatures: the same shape as minisign, so a
// release can also be checked by hand with `minisign -V`.
//
// A public key is "Ed" | key id (8 bytes) | Ed25519 public key (32 bytes),
// in base64. A signature file has four lines:
//
//	untrusted comment: <text>
//	base64("ED" | key id | Ed25519 signature of BLAKE2b-512(file))
//	trusted comment: <text>
//	base64(Ed25519 signature of (the signature above | the trusted text))
//
// Only "ED" (the file's hash is signed) is taken; minisign's older "Ed"
// (the whole file is signed) is refused, so one rule covers every file.
package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const (
	keyAlg = "Ed"
	sigAlg = "ED"

	untrustedPrefix = "untrusted comment: "
	trustedPrefix   = "trusted comment: "

	// maxSigFile bounds a signature file read from the network.
	maxSigFile = 4 << 10
)

// KeyID tells which key made a signature.
type KeyID [8]byte

// String is the id as minisign prints it.
func (id KeyID) String() string {
	return fmt.Sprintf("%016X", binary.LittleEndian.Uint64(id[:]))
}

// PublicKey is a key that checks signatures.
type PublicKey struct {
	ID  KeyID
	Key ed25519.PublicKey
}

// SecretKey makes signatures. It never leaves the signing tool's process;
// on disk it is locked (the tool's job, not this package's).
type SecretKey struct {
	ID  KeyID
	Key ed25519.PrivateKey
}

// NewKey makes a key pair with a random id.
func NewKey() (PublicKey, SecretKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PublicKey{}, SecretKey{}, err
	}
	var id KeyID
	if _, err := rand.Read(id[:]); err != nil {
		return PublicKey{}, SecretKey{}, err
	}
	return PublicKey{ID: id, Key: pub}, SecretKey{ID: id, Key: priv}, nil
}

// Public is the key that checks this key's signatures.
func (s SecretKey) Public() PublicKey {
	return PublicKey{ID: s.ID, Key: s.Key.Public().(ed25519.PublicKey)}
}

// Base64 is the key's one line, as in a minisign public key file.
func (k PublicKey) Base64() string {
	b := make([]byte, 0, 2+8+ed25519.PublicKeySize)
	b = append(b, keyAlg...)
	b = append(b, k.ID[:]...)
	b = append(b, k.Key...)
	return base64.StdEncoding.EncodeToString(b)
}

// File is the key as a minisign public key file.
func (k PublicKey) File() string {
	return untrustedPrefix + "minisign public key " + k.ID.String() + "\n" + k.Base64() + "\n"
}

// ParsePublicKey reads a key's base64 line, or a whole public key file.
func ParsePublicKey(s string) (PublicKey, error) {
	line := strings.TrimSpace(s)
	if strings.HasPrefix(line, untrustedPrefix) {
		_, rest, ok := strings.Cut(line, "\n")
		if !ok {
			return PublicKey{}, errors.New("public key file has no key line")
		}
		line = strings.TrimSpace(rest)
	}
	b, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(b) != 2+8+ed25519.PublicKeySize || string(b[:2]) != keyAlg {
		return PublicKey{}, errors.New("not an Ed25519 public key")
	}
	var k PublicKey
	copy(k.ID[:], b[2:10])
	k.Key = ed25519.PublicKey(bytes.Clone(b[10:]))
	return k, nil
}

// ErrBadSignature: the file, its signature or its trusted comment was
// changed, or another key signed it.
var ErrBadSignature = errors.New("the signature does not match")

// Sign makes the signature file of msg. trusted is signed with it; it
// must be one line.
func Sign(sk SecretKey, msg []byte, trusted string) ([]byte, error) {
	if strings.ContainsAny(trusted, "\r\n") {
		return nil, errors.New("the trusted comment must be one line")
	}
	h := blake2b.Sum512(msg)
	sig := ed25519.Sign(sk.Key, h[:])
	global := ed25519.Sign(sk.Key, append(bytes.Clone(sig), trusted...))
	b := make([]byte, 0, 2+8+ed25519.SignatureSize)
	b = append(b, sigAlg...)
	b = append(b, sk.ID[:]...)
	b = append(b, sig...)
	var out strings.Builder
	out.WriteString(untrustedPrefix + "signature from askorus-sign secret key\n")
	out.WriteString(base64.StdEncoding.EncodeToString(b) + "\n")
	out.WriteString(trustedPrefix + trusted + "\n")
	out.WriteString(base64.StdEncoding.EncodeToString(global) + "\n")
	return []byte(out.String()), nil
}

// Verify checks that one of keys signed msg, and returns the trusted
// comment and the key that signed it.
func Verify(keys []PublicKey, msg, sigFile []byte) (trusted string, by PublicKey, err error) {
	if len(sigFile) > maxSigFile {
		return "", PublicKey{}, errors.New("signature file too large")
	}
	lines := strings.Split(strings.ReplaceAll(string(sigFile), "\r\n", "\n"), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], untrustedPrefix) || !strings.HasPrefix(lines[2], trustedPrefix) {
		return "", PublicKey{}, errors.New("not a signature file")
	}
	for _, rest := range lines[4:] {
		if strings.TrimSpace(rest) != "" {
			return "", PublicKey{}, errors.New("not a signature file")
		}
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(b) != 2+8+ed25519.SignatureSize {
		return "", PublicKey{}, errors.New("not a signature file")
	}
	if string(b[:2]) != sigAlg {
		return "", PublicKey{}, fmt.Errorf("signature algorithm %q is not taken", b[:2])
	}
	var id KeyID
	copy(id[:], b[2:10])
	sig := b[10:]
	trusted = strings.TrimPrefix(lines[2], trustedPrefix)
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(global) != ed25519.SignatureSize {
		return "", PublicKey{}, errors.New("not a signature file")
	}
	for _, k := range keys {
		if k.ID != id {
			continue
		}
		h := blake2b.Sum512(msg)
		if !ed25519.Verify(k.Key, h[:], sig) || !ed25519.Verify(k.Key, append(bytes.Clone(sig), trusted...), global) {
			return "", PublicKey{}, ErrBadSignature
		}
		return trusted, k, nil
	}
	return "", PublicKey{}, fmt.Errorf("%w: key %s is not trusted", ErrBadSignature, id)
}

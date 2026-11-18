package main

// The two secret keys on disk (Phase 1 plan part 27):
//
//   - The root key is locked with its owner's password (scrypt, then
//     XChaCha20-Poly1305). It lives off the PC and is used only to vouch for
//     a release key.
//   - The release key is locked to this Windows user (DPAPI), so a release
//     can be signed without a password and only by this user on this PC.
//
// Neither secret is ever printed. A key file is never overwritten.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/heeo-dev/askorus/agent/internal/update"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/scrypt"
)

const (
	kindRootSecret    = "askorus-root-secret"
	kindReleaseSecret = "askorus-release-secret"

	// scrypt costs: about a second and 128 MiB, once per use of the root key.
	scryptN = 1 << 17
	scryptR = 8
	scryptP = 1

	minPassword = 12
)

// secretFile is a locked key as written on disk.
type secretFile struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`  // the key id, hex
	Pub  string `json:"pub"` // update.PublicKey.Base64

	// The root key: scrypt parameters, salt and the sealed seed.
	N     int    `json:"n,omitempty"`
	R     int    `json:"r,omitempty"`
	P     int    `json:"p,omitempty"`
	Salt  []byte `json:"salt,omitempty"`
	Nonce []byte `json:"nonce,omitempty"`
	Box   []byte `json:"box,omitempty"`

	// The release key: the seed as DPAPI locked it.
	DPAPI []byte `json:"dpapi,omitempty"`
}

// lockRoot seals the root key's seed with a key made from password, at
// scrypt cost n (scryptN; tests take less).
func lockRoot(sk update.SecretKey, password []byte, n int) (secretFile, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return secretFile{}, err
	}
	box, nonce, err := seal(sk, password, salt, n)
	if err != nil {
		return secretFile{}, err
	}
	pub := sk.Public()
	return secretFile{Kind: kindRootSecret, ID: hex.EncodeToString(sk.ID[:]), Pub: pub.Base64(), N: n, R: scryptR, P: scryptP, Salt: salt, Nonce: nonce, Box: box}, nil
}

func seal(sk update.SecretKey, password, salt []byte, n int) (box, nonce []byte, err error) {
	k, err := scrypt.Key(password, salt, n, scryptR, scryptP, chacha20poly1305.KeySize)
	if err != nil {
		return nil, nil, err
	}
	aead, err := chacha20poly1305.NewX(k)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	pub := sk.Public()
	// The public key is bound to the box: a box moved to another key's file does not open.
	return aead.Seal(nil, nonce, sk.Key.Seed(), []byte(pub.Base64())), nonce, nil
}

// errPassword: the password is wrong, or the file was changed.
var errPassword = errors.New("the password is wrong, or the key file was changed")

// unlockRoot opens a root key file with password.
func unlockRoot(f secretFile, password []byte) (update.SecretKey, error) {
	if f.Kind != kindRootSecret {
		return update.SecretKey{}, fmt.Errorf("not a root key (it is %q)", f.Kind)
	}
	// The costs are read from the file but kept in a sane range, so a
	// changed file cannot make this run for hours or take all memory.
	if f.N < 1<<14 || f.N > 1<<20 || f.R != scryptR || f.P != scryptP || len(f.Salt) != 32 {
		return update.SecretKey{}, errors.New("the root key file has unknown lock settings")
	}
	k, err := scrypt.Key(password, f.Salt, f.N, f.R, f.P, chacha20poly1305.KeySize)
	if err != nil {
		return update.SecretKey{}, err
	}
	aead, err := chacha20poly1305.NewX(k)
	if err != nil {
		return update.SecretKey{}, err
	}
	if len(f.Nonce) != aead.NonceSize() {
		return update.SecretKey{}, errPassword
	}
	seed, err := aead.Open(nil, f.Nonce, f.Box, []byte(f.Pub))
	if err != nil {
		return update.SecretKey{}, errPassword
	}
	return fromSeed(f, seed)
}

// lockRelease locks the release key's seed to this user with lock (DPAPI on Windows).
func lockRelease(sk update.SecretKey, lock func([]byte) ([]byte, error)) (secretFile, error) {
	sealed, err := lock(sk.Key.Seed())
	if err != nil {
		return secretFile{}, err
	}
	pub := sk.Public()
	return secretFile{Kind: kindReleaseSecret, ID: hex.EncodeToString(sk.ID[:]), Pub: pub.Base64(), DPAPI: sealed}, nil
}

// unlockRelease opens a release key file with unlock.
func unlockRelease(f secretFile, unlock func([]byte) ([]byte, error)) (update.SecretKey, error) {
	if f.Kind != kindReleaseSecret {
		return update.SecretKey{}, fmt.Errorf("not a release key (it is %q)", f.Kind)
	}
	seed, err := unlock(f.DPAPI)
	if err != nil {
		return update.SecretKey{}, fmt.Errorf("the release key does not open for this Windows user: %w", err)
	}
	return fromSeed(f, seed)
}

// fromSeed rebuilds the key and checks it is the one the file names.
func fromSeed(f secretFile, seed []byte) (update.SecretKey, error) {
	if len(seed) != ed25519.SeedSize {
		return update.SecretKey{}, errors.New("the key file holds no key")
	}
	pub, err := update.ParsePublicKey(f.Pub)
	if err != nil {
		return update.SecretKey{}, err
	}
	sk := update.SecretKey{ID: pub.ID, Key: ed25519.NewKeyFromSeed(seed)}
	if sk.Public().Base64() != pub.Base64() || hex.EncodeToString(pub.ID[:]) != f.ID {
		return update.SecretKey{}, errors.New("the key in the file is not the key it names")
	}
	return sk, nil
}

func readSecret(path string) (secretFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return secretFile{}, err
	}
	var f secretFile
	if err := json.Unmarshal(b, &f); err != nil {
		return secretFile{}, fmt.Errorf("%s is not a key file: %v", path, err)
	}
	return f, nil
}

// writeNew writes a file that must not exist yet, readable by this user only.
func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s is already there; a key file is never replaced", path)
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

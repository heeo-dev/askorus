package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// minisign's own release key, from its README ("Verification of Official
// Releases").
const minisignReleaseKey = "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"

// TestMinisignInterop checks the shape against the real minisign, when a
// folder with its release is given (it is not part of test-all):
//
//	ASKORUS_TEST_MINISIGN=<folder> go test ./internal/update -run Minisign
//
// The folder holds minisign-0.12-win64.zip and its .minisig as published;
// with minisign.exe there too (taken out of that zip once it checked), the
// other way is checked as well.
func TestMinisignInterop(t *testing.T) {
	dir := os.Getenv("ASKORUS_TEST_MINISIGN")
	if dir == "" {
		t.Skip("ASKORUS_TEST_MINISIGN is not set")
	}
	// minisign's signature of its own release passes this package's check.
	zip, err := os.ReadFile(filepath.Join(dir, "minisign-0.12-win64.zip"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(filepath.Join(dir, "minisign-0.12-win64.zip.minisig"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := ParsePublicKey(minisignReleaseKey)
	if err != nil {
		t.Fatal(err)
	}
	trusted, _, err := Verify([]PublicKey{key}, zip, sig)
	if err != nil {
		t.Fatalf("minisign's own release: %v", err)
	}
	t.Logf("minisign's release checks: %q", trusted)

	exe := filepath.Join(dir, "minisign.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skip("no minisign.exe to check the other way")
	}
	// A signature this package makes passes minisign -V.
	pub, sec := mustKey(t)
	file := filepath.Join(t.TempDir(), "release.json")
	if err := os.WriteFile(file, []byte(`{"kind":"askorus-release"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file+".minisig", mustSign(t, sec, []byte(`{"kind":"askorus-release"}`), ReleaseTrusted("0.5.0")), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(exe, "-Vm", file, "-P", pub.Base64()).CombinedOutput()
	if err != nil {
		t.Fatalf("minisign -V: %v\n%s", err, out)
	}
	t.Logf("minisign -V: %s", out)
}

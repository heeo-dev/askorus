package update

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

// MinKeySeq is the lowest release key number this build takes (R-140):
// raise it to the new number whenever a release key is replaced, so an
// agent whose update.json was deleted still refuses the old key. A var
// only so the tests can raise it.
var MinKeySeq = 0

// The root keys the agent is built with (roots/README.md).
//
//go:embed roots
var rootFiles embed.FS

// Roots are the public root keys this agent trusts, in file name order.
// A file that does not read as a key fails the build's tests, not an
// agent in use (TestEmbeddedRootsRead).
func Roots() ([]PublicKey, error) {
	entries, err := rootFiles.ReadDir("roots")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".pub") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var keys []PublicKey
	for _, n := range names {
		b, err := rootFiles.ReadFile(path.Join("roots", n))
		if err != nil {
			return nil, err
		}
		k, err := ParsePublicKey(string(b))
		if err != nil {
			return nil, fmt.Errorf("root key %s: %w", n, err)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

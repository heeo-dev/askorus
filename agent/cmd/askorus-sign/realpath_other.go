//go:build !windows

package main

import "path/filepath"

// finalPath is where an existing file or folder really is, links followed.
func finalPath(p string) (string, error) { return filepath.EvalSymlinks(p) }

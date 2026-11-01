//go:build judgeeval && !windows

package main

// elevated is false off Windows, as claude.SelfElevated is.
func elevated() bool { return false }

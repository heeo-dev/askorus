//go:build !windows

package main

import "errors"

// Releases are made on the maintainer's Windows PC (Phase 1 plan part 27);
// elsewhere the release key cannot be locked or opened.
var errNoDPAPI = errors.New("the release key is kept with Windows DPAPI; make and use it on Windows")

func dpapiLock([]byte) ([]byte, error)   { return nil, errNoDPAPI }
func dpapiUnlock([]byte) ([]byte, error) { return nil, errNoDPAPI }

//go:build judgeeval && windows

package main

import "golang.org/x/sys/windows"

// elevated reports whether this process runs as administrator, as
// claude.SelfElevated does: the tool and its AIs run with normal rights
// (구현 전 테스트 6).
func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

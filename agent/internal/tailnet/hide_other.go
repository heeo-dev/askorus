//go:build !windows

package tailnet

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}

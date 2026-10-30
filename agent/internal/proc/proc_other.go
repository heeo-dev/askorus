//go:build !windows

package proc

import "os/exec"

func prepare(cmd *exec.Cmd) {}

// contain is a no-op outside Windows for now; exec ends the process itself
// when the context ends.
func contain(cmd *exec.Cmd) func() { return func() {} }

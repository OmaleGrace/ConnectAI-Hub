//go:build !windows

package web

import (
	"os/exec"
	"syscall"
)

// Chrome starts helper processes; run them in their own process group so the
// whole tree can be stopped when a screenshot times out.
func setProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

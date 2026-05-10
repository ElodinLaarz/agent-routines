//go:build !windows

package adapter

import (
	"os"
	"os/exec"
	"syscall"
)

func setNewProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func osEnviron() []string { return os.Environ() }

// envKey returns the case-folded key used to dedup environment variables.
// POSIX env-var names are case-sensitive — return the key unchanged.
func envKey(k string) string { return k }

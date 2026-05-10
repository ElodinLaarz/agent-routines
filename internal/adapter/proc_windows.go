//go:build windows

package adapter

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func setNewProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
}

func osEnviron() []string { return os.Environ() }

// envKey returns the case-folded key used to dedup environment variables.
// Windows env-var names are case-insensitive (Path == PATH), so fold to
// upper-case so the dedup contract holds.
func envKey(k string) string { return strings.ToUpper(k) }

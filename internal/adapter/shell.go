package adapter

import (
	"context"
	"fmt"
	"os/exec"
)

// Shell is the generic escape-hatch adapter — runs an arbitrary command
// with the optional prompt piped to stdin.
type Shell struct{}

func (Shell) Name() string { return "shell" }

func (s Shell) Run(ctx context.Context, r Request) (Result, error) {
	if len(r.Command) == 0 {
		return Result{ExitCode: -1}, fmt.Errorf("shell adapter: command is required")
	}
	cmd := exec.CommandContext(ctx, r.Command[0], r.Command[1:]...) //nolint:gosec
	return runCmd(ctx, cmd, r, r.Prompt)
}

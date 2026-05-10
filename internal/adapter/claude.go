package adapter

import (
	"context"
	"fmt"
	"os/exec"
)

// Claude wraps the Claude Code CLI in headless mode (`claude -p`).
// Honors ANTHROPIC_API_KEY (or the user's `claude login` session).
type Claude struct {
	// Bin overrides the binary name; defaults to "claude".
	Bin string
	// ExtraArgs is appended after the prompt argument (e.g. ["--output-format", "stream-json"]).
	ExtraArgs []string
}

func (c Claude) Name() string { return "claude" }

func (c Claude) Run(ctx context.Context, r Request) (Result, error) {
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}
	if r.Prompt == "" {
		return Result{ExitCode: -1}, fmt.Errorf("claude adapter: prompt is required")
	}
	args := append([]string{"-p", r.Prompt}, c.ExtraArgs...)
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec
	return runCmd(ctx, cmd, r, "")
}

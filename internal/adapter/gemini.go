package adapter

import (
	"context"
	"fmt"
	"os/exec"
)

// Gemini wraps the gemini CLI in non-interactive mode.
//
// Invocation: `gemini -p "<prompt>"`. Honors GEMINI_API_KEY and any user env.
type Gemini struct {
	// Bin is the binary name or absolute path; defaults to "gemini".
	Bin string
}

func (g Gemini) Name() string { return "gemini" }

func (g Gemini) Run(ctx context.Context, r Request) (Result, error) {
	bin := g.Bin
	if bin == "" {
		bin = "gemini"
	}
	if r.Prompt == "" {
		return Result{ExitCode: -1}, fmt.Errorf("gemini adapter: prompt is required")
	}
	cmd := exec.CommandContext(ctx, bin, "-p", r.Prompt) //nolint:gosec
	return runCmd(ctx, cmd, r, "")
}

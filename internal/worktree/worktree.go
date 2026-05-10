// Package worktree creates and tears down git worktrees so a routine
// can fire in an isolated working directory each run.
package worktree

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Setup describes one worktree to create.
type Setup struct {
	// RepoRoot is any directory inside the target git repository.
	RepoRoot string
	// BranchName is the branch checked out in the new worktree. Required.
	BranchName string
	// Path is the worktree directory; either absolute or relative to
	// the repo's top-level directory.
	Path string
	// PostCreate is an optional shell command run inside the worktree
	// before the routine fires. Stdout and stderr are forwarded to W.
	PostCreate string
	// W mirrors all subprocess stdout/stderr (worktree creation, git,
	// post-create). Lets the caller capture it in the per-run log.
	W io.Writer
}

// Result is what Create returns.
type Result struct {
	// Path is the absolute worktree directory.
	Path string
	// Branch is the branch the worktree was checked out on.
	Branch string
	// Cleanup is best-effort: removes the worktree dir and deletes the
	// branch. Safe to call multiple times.
	Cleanup func() error
}

// Create runs `git worktree add` plus an optional post-create hook.
// On error, partially-created state is best-effort cleaned up so the
// caller does not have to.
func Create(ctx context.Context, s Setup) (*Result, error) {
	if s.RepoRoot == "" {
		return nil, fmt.Errorf("worktree: RepoRoot is required")
	}
	if s.BranchName == "" {
		return nil, fmt.Errorf("worktree: BranchName is required")
	}
	top, err := repoTopLevel(ctx, s.RepoRoot, s.W)
	if err != nil {
		return nil, fmt.Errorf("locate repo root: %w", err)
	}

	wtPath := s.Path
	if wtPath == "" {
		return nil, fmt.Errorf("worktree: Path is required")
	}
	if !filepath.IsAbs(wtPath) {
		wtPath = filepath.Join(top, wtPath)
	}

	if err := runGit(ctx, top, s.W, "worktree", "add", "-b", s.BranchName, wtPath); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	cleanup := func() error {
		// Detach from the caller's ctx so a routine that timed out, was
		// canceled, or was killed during shutdown still gets its worktree
		// and branch cleaned up. Bound by an internal 30s deadline so a
		// hung git invocation can't pin shutdown forever.
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// remove --force handles uncommitted changes the routine may have left.
		_ = runGit(cctx, top, s.W, "worktree", "remove", "--force", wtPath)
		_ = runGit(cctx, top, s.W, "branch", "-D", s.BranchName)
		return nil
	}

	if s.PostCreate != "" {
		if err := runShell(ctx, wtPath, s.W, s.PostCreate); err != nil {
			_ = cleanup()
			return nil, fmt.Errorf("post_create: %w", err)
		}
	}

	return &Result{Path: wtPath, Branch: s.BranchName, Cleanup: cleanup}, nil
}

func repoTopLevel(ctx context.Context, dir string, w io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func runGit(ctx context.Context, cwd string, w io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func runShell(ctx context.Context, cwd string, w io.Writer, script string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", script) //nolint:gosec
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", script) //nolint:gosec
	}
	cmd.Dir = cwd
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

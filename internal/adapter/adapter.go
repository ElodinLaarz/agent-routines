// Package adapter exposes a uniform interface for invoking agent CLIs.
package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Request is what every adapter receives.
type Request struct {
	Prompt  string
	Workdir string
	Env     map[string]string
	Timeout time.Duration
	Stdout  io.Writer
	Stderr  io.Writer
	// Command is honored only by the shell adapter.
	Command []string
}

// Result describes the outcome of one invocation.
type Result struct {
	ExitCode int
	Duration time.Duration
}

// Adapter is the contract every agent backend implements.
type Adapter interface {
	Name() string
	Run(ctx context.Context, r Request) (Result, error)
}

// ErrTimeout is wrapped into the error returned by Run when the request's
// Timeout fires before the child process exits. Callers can classify a
// timeout cleanly with errors.Is(err, adapter.ErrTimeout) instead of
// pattern-matching on error strings.
var ErrTimeout = errors.New("adapter timeout")

// Registry holds adapters by name.
type Registry struct{ m map[string]Adapter }

// NewRegistry returns a new empty Registry.
func NewRegistry() *Registry { return &Registry{m: map[string]Adapter{}} }

// Register adds a to the registry under its name.
func (r *Registry) Register(a Adapter) { r.m[a.Name()] = a }

// Get returns the adapter registered under name, or an error if not found.
func (r *Registry) Get(name string) (Adapter, error) {
	a, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", name)
	}
	return a, nil
}

// runCmd executes `cmd` with the request's plumbing and honors timeout/cancel.
// Stdout/Stderr stream to the request writers; if nil, output is dropped.
// On timeout the process tree is killed.
func runCmd(ctx context.Context, cmd *exec.Cmd, r Request, stdin string) (Result, error) {
	start := time.Now()
	cmd.Dir = r.Workdir
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Env = mergeEnv(r.Env)
	setNewProcessGroup(cmd)

	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Duration: time.Since(start)}, fmt.Errorf("start: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		killProcessTree(cmd)
		<-done
		dur := time.Since(start)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Result{ExitCode: -1, Duration: dur}, fmt.Errorf("after %s: %w", r.Timeout, ErrTimeout)
		}
		return Result{ExitCode: -1, Duration: dur}, ctx.Err()
	case err := <-done:
		dur := time.Since(start)
		if err == nil {
			return Result{ExitCode: 0, Duration: dur}, nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return Result{ExitCode: ee.ExitCode(), Duration: dur}, nil
		}
		return Result{ExitCode: -1, Duration: dur}, err
	}
}

// mergeEnv returns the process environment overlaid with `custom`, with
// duplicate keys collapsed so callers see exactly one entry per name.
// Without this, child processes can inherit two `KEY=...` lines and
// which one "wins" becomes platform/process-dependent.
//
// Windows env-var names are case-insensitive — `Path` and `PATH` refer
// to the same variable. We normalize to upper-case while merging on
// Windows so the dedup contract holds. Original casing of the surviving
// entry is preserved.
func mergeEnv(custom map[string]string) []string {
	type entry struct{ k, v string }
	merged := map[string]entry{}
	for _, kv := range osEnviron() {
		eq := indexEq(kv)
		if eq <= 0 {
			continue
		}
		k := kv[:eq]
		merged[envKey(k)] = entry{k, kv[eq+1:]}
	}
	for k, v := range custom {
		merged[envKey(k)] = entry{k, v}
	}
	out := make([]string, 0, len(merged))
	for _, e := range merged {
		out = append(out, e.k+"="+e.v)
	}
	return out
}

func indexEq(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return i
		}
	}
	return -1
}

// killProcessTree and setNewProcessGroup live in platform-specific files.

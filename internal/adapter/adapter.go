// Package adapter exposes a uniform interface for invoking agent CLIs.
package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
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

// Registry holds adapters by name.
type Registry struct{ m map[string]Adapter }

func NewRegistry() *Registry { return &Registry{m: map[string]Adapter{}} }

func (r *Registry) Register(a Adapter) { r.m[a.Name()] = a }

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
		cmd.Stdin = stringReader(stdin)
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
			return Result{ExitCode: -1, Duration: dur}, fmt.Errorf("timeout after %s", r.Timeout)
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

func stringReader(s string) io.Reader { return &strReader{s: s} }

type strReader struct {
	s string
	i int
}

func (r *strReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

// mergeEnv returns the process environment overlaid with `custom`, with
// duplicate keys collapsed so callers see exactly one entry per name.
// Without this, child processes can inherit two `KEY=...` lines and
// which one "wins" becomes platform/process-dependent.
func mergeEnv(custom map[string]string) []string {
	merged := map[string]string{}
	for _, kv := range osEnviron() {
		eq := indexEq(kv)
		if eq <= 0 {
			continue
		}
		merged[kv[:eq]] = kv[eq+1:]
	}
	for k, v := range custom {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
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

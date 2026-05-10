package adapter

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellEcho(t *testing.T) {
	cmd := []string{"echo", "hello"}
	if runtime.GOOS == "windows" {
		cmd = []string{"cmd", "/c", "echo hello"}
	}
	var buf bytes.Buffer
	res, err := Shell{}.Run(context.Background(), Request{
		Command: cmd,
		Stdout:  &buf,
		Stderr:  &buf,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit=%d", res.ExitCode)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("missing 'hello' in: %q", buf.String())
	}
}

func TestShellTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep semantics differ on Windows")
	}
	res, err := Shell{}.Run(context.Background(), Request{
		Command: []string{"sleep", "60"},
		Timeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit on timeout")
	}
	if res.Duration > 5*time.Second {
		t.Errorf("kill took too long: %s", res.Duration)
	}
}

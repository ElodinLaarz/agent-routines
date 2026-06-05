package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLastPerRoutineHandlesRunningRunWithNullError(t *testing.T) {
	h := openTestHistory(t)

	_, err := h.Begin("nightly", time.Now(), "nightly.log")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	runs, err := h.LastPerRoutine()
	if err != nil {
		t.Fatalf("LastPerRoutine: %v", err)
	}
	if got := runs["nightly"].Status; got != "running" {
		t.Fatalf("status = %q, want running", got)
	}
	if got := runs["nightly"].Error; got != "" {
		t.Fatalf("error = %q, want empty string", got)
	}
}

func TestLastNHandlesSkippedRunWithNullLogPath(t *testing.T) {
	h := openTestHistory(t)

	err := h.Skip("nightly", time.Now(), "already running")
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}

	runs, err := h.LastN("nightly", 1)
	if err != nil {
		t.Fatalf("LastN: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	if got := runs[0].LogPath; got != "" {
		t.Fatalf("log path = %q, want empty string", got)
	}
	if got := runs[0].Error; got != "already running" {
		t.Fatalf("error = %q, want already running", got)
	}
}

func openTestHistory(t *testing.T) *History {
	t.Helper()

	h, err := OpenHistory(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenHistory: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return h
}

// Package log writes per-run output to ~/.routines/logs/{routine}/{ISO8601}.log
// and prunes old runs per a retention policy.
package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Writer captures stdout+stderr of one run.
type Writer struct {
	Path  string
	file  *os.File
	mu    sync.Mutex
	tee   io.Writer
	extra []io.Writer
}

// New opens a log file at <dir>/<routine>/<startedAt>.log. Caller must Close.
func New(rootDir, routine string, startedAt time.Time, mirrors ...io.Writer) (*Writer, error) {
	d := filepath.Join(rootDir, routine)
	// Routine output may include sensitive material (agent responses,
	// command output). Restrict to owner only by default.
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	name := startedAt.UTC().Format("20060102T150405Z") + ".log"
	p := filepath.Join(d, name)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	w := &Writer{Path: p, file: f, extra: mirrors}
	if len(mirrors) == 0 {
		w.tee = f
	} else {
		w.tee = io.MultiWriter(append([]io.Writer{f}, mirrors...)...)
	}
	return w, nil
}

// Write satisfies io.Writer; safe for concurrent use.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tee.Write(p)
}

// Close finalizes the file and updates the routine's `latest.log` symlink.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	dir := filepath.Dir(w.Path)
	link := filepath.Join(dir, "latest.log")
	_ = os.Remove(link)
	// Symlinks may fail on Windows without privileges; ignore the error.
	_ = os.Symlink(filepath.Base(w.Path), link)
	return nil
}

// Prune retains the last `keepLastN` log files per routine; pass 0 to skip
// count-based pruning. If `maxAge` > 0, also deletes files older than maxAge.
// Returns the list of files removed.
func Prune(rootDir, routine string, keepLastN int, maxAge time.Duration) ([]string, error) {
	dir := filepath.Join(rootDir, routine)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	type fi struct {
		path string
		mod  time.Time
	}
	var logs []fi
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".log" || e.Name() == "latest.log" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		logs = append(logs, fi{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	// newest first
	sort.Slice(logs, func(i, j int) bool { return logs[i].mod.After(logs[j].mod) })

	var removed []string
	for i, l := range logs {
		drop := false
		if keepLastN > 0 && i >= keepLastN {
			drop = true
		}
		if maxAge > 0 && time.Since(l.mod) > maxAge {
			drop = true
		}
		if drop {
			if err := os.Remove(l.path); err == nil {
				removed = append(removed, l.path)
			}
		}
	}
	return removed, nil
}

// FormatHeader returns a nicely-prefixed banner written at log start.
func FormatHeader(routine string, startedAt time.Time) string {
	return fmt.Sprintf("=== routine=%s started=%s ===\n", routine, startedAt.UTC().Format(time.RFC3339))
}

// Tail returns the last n lines of the log file at path. Returns an empty
// string on error or if path is empty.
func Tail(path string, n int) string {
	if path == "" || n <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	const maxRead = 64 * 1024
	if size := fi.Size(); size > maxRead {
		if _, err := f.Seek(size-maxRead, io.SeekStart); err != nil {
			return ""
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	trimmed := strings.TrimRight(string(data), "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

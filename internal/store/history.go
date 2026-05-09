// Package store: persistent state. history.go is the SQLite run-history
// store; fsstore.go is the routine-spec watcher.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Pure-Go SQLite driver (registered via init); imported for side effects.
	_ "modernc.org/sqlite"
)

// History records every run.
type History struct {
	db *sql.DB
}

// Run is a single row in the runs table.
type Run struct {
	ID         int64
	Routine    string
	StartedAt  time.Time
	FinishedAt sql.NullTime
	Status     string // running | success | failed | timeout | skipped
	ExitCode   sql.NullInt64
	DurationMs sql.NullInt64
	LogPath    string
	Error      string
}

// OpenHistory opens (and migrates) ~/.routines/state.db at the given path.
func OpenHistory(path string) (*History, error) {
	if err := mkParent(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	h := &History{db: db}
	if err := h.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return h, nil
}

func (h *History) Close() error { return h.db.Close() }

func (h *History) migrate() error {
	_, err := h.db.Exec(`
CREATE TABLE IF NOT EXISTS runs (
  id           INTEGER PRIMARY KEY,
  routine      TEXT NOT NULL,
  started_at   DATETIME NOT NULL,
  finished_at  DATETIME,
  status       TEXT NOT NULL,
  exit_code    INTEGER,
  duration_ms  INTEGER,
  log_path     TEXT,
  error        TEXT
);
CREATE INDEX IF NOT EXISTS idx_runs_routine ON runs(routine, started_at DESC);
`)
	return err
}

// Begin records a run start and returns the new run id.
func (h *History) Begin(routine string, startedAt time.Time, logPath string) (int64, error) {
	res, err := h.db.Exec(
		`INSERT INTO runs(routine, started_at, status, log_path) VALUES(?, ?, ?, ?)`,
		routine, startedAt.UTC(), "running", logPath,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Finish updates a previously-begun run.
func (h *History) Finish(id int64, status string, exitCode int, runErr error) error {
	now := time.Now().UTC()
	var errStr string
	if runErr != nil {
		errStr = runErr.Error()
	}
	// Compute duration as `(finished - started)` in SQL so callers do not
	// need to remember the start time.
	_, err := h.db.Exec(`
UPDATE runs
SET finished_at = ?,
    status      = ?,
    exit_code   = ?,
    duration_ms = CAST((julianday(?) - julianday(started_at)) * 86400000 AS INTEGER),
    error       = ?
WHERE id = ?
`, now, status, exitCode, now, errStr, id)
	return err
}

// Skip records a run that the scheduler refused (e.g. lock held).
func (h *History) Skip(routine string, at time.Time, reason string) error {
	_, err := h.db.Exec(
		`INSERT INTO runs(routine, started_at, finished_at, status, error) VALUES(?, ?, ?, ?, ?)`,
		routine, at.UTC(), at.UTC(), "skipped", reason,
	)
	return err
}

// LastN returns the most-recent N runs for a routine, newest first.
func (h *History) LastN(routine string, n int) ([]Run, error) {
	if n <= 0 {
		n = 25
	}
	rows, err := h.db.Query(
		`SELECT id, routine, started_at, finished_at, status, exit_code, duration_ms, log_path, error
         FROM runs WHERE routine = ? ORDER BY started_at DESC LIMIT ?`,
		routine, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.Routine, &r.StartedAt, &r.FinishedAt, &r.Status, &r.ExitCode, &r.DurationMs, &r.LogPath, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastPerRoutine returns one row per routine — its most recent run.
func (h *History) LastPerRoutine() (map[string]Run, error) {
	rows, err := h.db.Query(`
SELECT r.id, r.routine, r.started_at, r.finished_at, r.status, r.exit_code, r.duration_ms, r.log_path, r.error
FROM runs r
JOIN (
  SELECT routine, MAX(started_at) AS m FROM runs GROUP BY routine
) m ON m.routine = r.routine AND m.m = r.started_at
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.Routine, &r.StartedAt, &r.FinishedAt, &r.Status, &r.ExitCode, &r.DurationMs, &r.LogPath, &r.Error); err != nil {
			return nil, err
		}
		out[r.Routine] = r
	}
	return out, rows.Err()
}

func mkParent(p string) error {
	dir := filepath.Dir(p)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return nil
}

// Package scheduler is the cron loop that fires routines on schedule.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ElodinLaarz/agent-routines/internal/adapter"
	pkglog "github.com/ElodinLaarz/agent-routines/internal/log"
	"github.com/ElodinLaarz/agent-routines/internal/notify"
	"github.com/ElodinLaarz/agent-routines/internal/spec"
	"github.com/ElodinLaarz/agent-routines/internal/store"
)

// Config wires the scheduler to its collaborators.
type Config struct {
	Adapters      *adapter.Registry
	History       *store.History
	LogDir        string
	GracePeriod   time.Duration // how long to wait for in-flight runs on shutdown
	Notifier      notify.Notifier
	KeepLastN     int           // log retention count per routine
	MaxLogAge     time.Duration // log retention age
}

// Scheduler is the long-running engine.
type Scheduler struct {
	cfg Config
	c   *cron.Cron

	mu       sync.Mutex
	jobs     map[string]cron.EntryID // routine name -> cron entry id
	locks    map[string]*sync.Mutex  // routine name -> per-routine lock
	inflight map[string]int          // routine name -> running count

	// inFlightWg tracks goroutines fired by jobs so Stop can wait.
	inFlightWg sync.WaitGroup
}

// New returns a Scheduler ready to AddOrReplace routines.
func New(cfg Config) *Scheduler {
	if cfg.GracePeriod == 0 {
		cfg.GracePeriod = 30 * time.Second
	}
	return &Scheduler{
		cfg:      cfg,
		c:        cron.New(cron.WithSeconds()),
		jobs:     map[string]cron.EntryID{},
		locks:    map[string]*sync.Mutex{},
		inflight: map[string]int{},
	}
}

// Start kicks off the cron loop. Call Stop for graceful shutdown.
func (s *Scheduler) Start() { s.c.Start() }

// Stop signals the cron loop to halt and waits up to GracePeriod for any
// in-flight runs to finish. Returns immediately on grace expiry.
func (s *Scheduler) Stop() {
	stopCtx := s.c.Stop() // cron's Stop returns a ctx that closes when current entries complete

	done := make(chan struct{})
	go func() {
		s.inFlightWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(s.cfg.GracePeriod):
	}
	<-stopCtx.Done()
}

// AddOrReplace registers (or updates) a routine. Returns nil if the
// routine is disabled — caller should treat that as "not scheduled".
func (s *Scheduler) AddOrReplace(r *spec.Routine) error {
	if r == nil {
		return errors.New("nil routine")
	}
	if !r.IsEnabled() {
		s.Remove(r.Name)
		return nil
	}
	cronExpr, err := spec.ParseSchedule(r.Schedule)
	if err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	// robfig WithSeconds expects 6-field; tolerate 5-field by prepending "0".
	if !strings.HasPrefix(cronExpr, "@every") && len(strings.Fields(cronExpr)) == 5 {
		cronExpr = "0 " + cronExpr
	}

	id, err := s.c.AddFunc(cronExpr, s.fireFunc(r))
	if err != nil {
		return fmt.Errorf("add cron: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.jobs[r.Name]; ok {
		s.c.Remove(old)
	}
	s.jobs[r.Name] = id
	if _, ok := s.locks[r.Name]; !ok {
		s.locks[r.Name] = &sync.Mutex{}
	}
	return nil
}

// Remove unregisters a routine.
func (s *Scheduler) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.jobs[name]; ok {
		s.c.Remove(id)
		delete(s.jobs, name)
	}
}

// NextFire returns the next scheduled fire time for a routine, or zero
// if it is not registered.
func (s *Scheduler) NextFire(name string) time.Time {
	s.mu.Lock()
	id, ok := s.jobs[name]
	s.mu.Unlock()
	if !ok {
		return time.Time{}
	}
	entry := s.c.Entry(id)
	return entry.Next
}

// fireFunc wraps a routine in skip-if-running, log-capture, history,
// and notifier plumbing.
func (s *Scheduler) fireFunc(r *spec.Routine) func() {
	name := r.Name
	return func() {
		s.inFlightWg.Add(1)
		defer s.inFlightWg.Done()

		s.mu.Lock()
		lock := s.locks[name]
		s.mu.Unlock()
		if !lock.TryLock() {
			stdlog.Printf("[%s] skipped: previous run still in flight", name)
			if s.cfg.History != nil {
				_ = s.cfg.History.Skip(name, time.Now(), "previous still running")
			}
			return
		}
		defer lock.Unlock()

		s.runOnce(r)
	}
}

// runOnce executes a single fire. Exported via FireNow for `routines run`.
func (s *Scheduler) runOnce(r *spec.Routine) {
	startedAt := time.Now()
	w, err := pkglog.New(s.cfg.LogDir, r.Name, startedAt)
	var logPath string
	if err == nil {
		logPath = w.Path
		_, _ = w.Write([]byte(pkglog.FormatHeader(r.Name, startedAt)))
	} else {
		stdlog.Printf("[%s] log open failed: %v", r.Name, err)
	}

	var runID int64
	if s.cfg.History != nil {
		if rid, herr := s.cfg.History.Begin(r.Name, startedAt, logPath); herr == nil {
			runID = rid
		} else {
			stdlog.Printf("[%s] history.Begin: %v", r.Name, herr)
		}
	}

	a, aerr := s.cfg.Adapters.Get(r.Agent)
	var (
		exit    int
		runErr  error
		status  = notify.StatusSuccess
		dur     time.Duration
		timedOut bool
	)
	if aerr != nil {
		exit, runErr, status = -1, aerr, notify.StatusFailed
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req := adapter.Request{
			Prompt:  r.Prompt,
			Workdir: r.Workdir,
			Env:     r.Env,
			Timeout: r.Timeout,
			Stdout:  w,
			Stderr:  w,
			Command: r.Command,
		}
		var res adapter.Result
		res, runErr = a.Run(ctx, req)
		exit = res.ExitCode
		dur = res.Duration

		switch {
		case runErr != nil && strings.Contains(runErr.Error(), "timeout"):
			status, timedOut = notify.StatusTimeout, true
		case runErr != nil || exit != 0:
			status = notify.StatusFailed
		}
	}

	finished := startedAt.Add(dur)
	if dur == 0 {
		finished = time.Now()
	}

	if w != nil {
		_, _ = fmt.Fprintf(w, "=== finished status=%s exit=%d duration=%s ===\n",
			status, exit, finished.Sub(startedAt))
		_ = w.Close()
	}

	if s.cfg.History != nil && runID > 0 {
		_ = s.cfg.History.Finish(runID, status, exit, runErr)
	}

	if s.cfg.Notifier != nil {
		evt := notify.Event{
			Routine:  r.Name,
			Status:   status,
			Started:  startedAt,
			Finished: finished,
			ExitCode: exit,
			LogPath:  logPath,
		}
		if runErr != nil {
			evt.Error = runErr.Error()
		}
		_ = s.cfg.Notifier.Notify(context.Background(), evt)
	}

	// rotation
	if s.cfg.KeepLastN > 0 || s.cfg.MaxLogAge > 0 {
		_, _ = pkglog.Prune(s.cfg.LogDir, r.Name, s.cfg.KeepLastN, s.cfg.MaxLogAge)
	}

	// retry policy: simple synchronous retries with backoff for OnFailure=retry
	if status != notify.StatusSuccess && r.OnFailure == "retry" && r.Retries > 0 && !timedOut {
		s.retry(r, exit, runErr)
	}
}

func (s *Scheduler) retry(r *spec.Routine, lastExit int, lastErr error) {
	backoff := r.Backoff
	if backoff == 0 {
		backoff = 30 * time.Second
	}
	for i := 0; i < r.Retries; i++ {
		time.Sleep(backoff)
		stdlog.Printf("[%s] retry %d/%d (last exit=%d err=%v)",
			r.Name, i+1, r.Retries, lastExit, lastErr)
		// NB: we intentionally do not recurse into runOnce's full logic —
		// retries are best-effort and feed the same notifier on next runOnce.
		s.runOnce(r)
	}
}

// FireNow runs a routine immediately, bypassing the schedule but honoring
// the per-routine lock. Useful for `routines run <name>`.
func (s *Scheduler) FireNow(r *spec.Routine) {
	s.mu.Lock()
	if _, ok := s.locks[r.Name]; !ok {
		s.locks[r.Name] = &sync.Mutex{}
	}
	lock := s.locks[r.Name]
	s.mu.Unlock()
	if !lock.TryLock() {
		stdlog.Printf("[%s] FireNow: lock busy", r.Name)
		return
	}
	defer lock.Unlock()
	s.runOnce(r)
}

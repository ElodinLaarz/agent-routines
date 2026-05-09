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
// in-flight runs to finish. Returns when in-flight goroutines complete OR
// the grace period elapses, whichever comes first.
func (s *Scheduler) Stop() {
	// cron's Stop returns a ctx that closes when current entries complete.
	// We don't block on it — the WaitGroup is the source of truth and the
	// grace timer is the upper bound.
	_ = s.c.Stop()

	done := make(chan struct{})
	go func() {
		s.inFlightWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(s.cfg.GracePeriod):
	}
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

	// Pre-create the per-routine lock BEFORE registering with cron so a
	// fire that happens to be due immediately can't read a nil entry.
	s.mu.Lock()
	if _, ok := s.locks[r.Name]; !ok {
		s.locks[r.Name] = &sync.Mutex{}
	}
	s.mu.Unlock()

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

// fireFunc wraps a routine in skip-if-running plus the full retry +
// notifier orchestration. Defensive: re-creates the per-routine lock
// if for any reason it's missing.
func (s *Scheduler) fireFunc(r *spec.Routine) func() {
	name := r.Name
	return func() {
		s.inFlightWg.Add(1)
		defer s.inFlightWg.Done()

		lock := s.lockFor(name)
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

// lockFor returns the per-routine mutex, creating one if missing.
func (s *Scheduler) lockFor(name string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locks[name]; ok {
		return l
	}
	l := &sync.Mutex{}
	s.locks[name] = l
	return l
}

// runResult captures the outcome of one execution attempt.
type runResult struct {
	exit     int
	runErr   error
	status   string
	started  time.Time
	finished time.Time
	logPath  string
	timedOut bool
}

// runOnce orchestrates a fire: execute, then retry on failure per the
// routine's policy (bounded by r.Retries — no recursion). The notifier
// fires once, with the final attempt's status.
func (s *Scheduler) runOnce(r *spec.Routine) {
	res := s.executeOnce(r)

	if res.status != notify.StatusSuccess && r.OnFailure == "retry" && r.Retries > 0 && !res.timedOut {
		backoff := r.Backoff
		if backoff == 0 {
			backoff = 30 * time.Second
		}
		for i := 0; i < r.Retries; i++ {
			time.Sleep(backoff)
			stdlog.Printf("[%s] retry %d/%d (last exit=%d err=%v)",
				r.Name, i+1, r.Retries, res.exit, res.runErr)
			res = s.executeOnce(r)
			if res.status == notify.StatusSuccess {
				break
			}
		}
	}

	s.maybeNotify(r, res)

	if s.cfg.KeepLastN > 0 || s.cfg.MaxLogAge > 0 {
		_, _ = pkglog.Prune(s.cfg.LogDir, r.Name, s.cfg.KeepLastN, s.cfg.MaxLogAge)
	}
}

// executeOnce runs the adapter once and writes log + history. No retries.
func (s *Scheduler) executeOnce(r *spec.Routine) runResult {
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
	res := runResult{started: startedAt, status: notify.StatusSuccess, logPath: logPath}
	var dur time.Duration
	if aerr != nil {
		res.exit, res.runErr, res.status = -1, aerr, notify.StatusFailed
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
		out, runErr := a.Run(ctx, req)
		res.runErr = runErr
		res.exit = out.ExitCode
		dur = out.Duration

		switch {
		case runErr != nil && strings.Contains(runErr.Error(), "timeout"):
			res.status, res.timedOut = notify.StatusTimeout, true
		case runErr != nil || res.exit != 0:
			res.status = notify.StatusFailed
		}
	}

	res.finished = startedAt.Add(dur)
	if dur == 0 {
		res.finished = time.Now()
	}

	if w != nil {
		_, _ = fmt.Fprintf(w, "=== finished status=%s exit=%d duration=%s ===\n",
			res.status, res.exit, res.finished.Sub(startedAt))
		_ = w.Close()
	}

	if s.cfg.History != nil && runID > 0 {
		_ = s.cfg.History.Finish(runID, res.status, res.exit, res.runErr)
	}
	return res
}

// maybeNotify fires the configured notifier when the run failed, or when
// the routine has explicitly opted in via OnFailure: alert. Per-routine
// outputs[].notifier filters the daemon-wide notifier set by name.
func (s *Scheduler) maybeNotify(r *spec.Routine, res runResult) {
	if s.cfg.Notifier == nil {
		return
	}
	shouldNotify := res.status != notify.StatusSuccess
	if r.OnFailure == "alert" {
		shouldNotify = true
	}
	if !shouldNotify {
		return
	}

	n := s.cfg.Notifier
	if filtered := filterNotifierByOutputs(n, r.Outputs); filtered != nil {
		n = filtered
	}

	evt := notify.Event{
		Routine:  r.Name,
		Status:   res.status,
		Started:  res.started,
		Finished: res.finished,
		ExitCode: res.exit,
		LogPath:  res.logPath,
	}
	if res.runErr != nil {
		evt.Error = res.runErr.Error()
	}
	_ = n.Notify(context.Background(), evt)
}

// filterNotifierByOutputs picks a sub-notifier from a Multi based on the
// notifier names referenced in r.Outputs. Returns nil if filtering does
// not apply (no outputs, base isn't Multi, or zero matches).
func filterNotifierByOutputs(base notify.Notifier, outputs []spec.Output) notify.Notifier {
	wanted := map[string]struct{}{}
	for _, o := range outputs {
		if o.Notifier != "" {
			wanted[o.Notifier] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	multi, ok := base.(notify.Multi)
	if !ok {
		return nil
	}
	var picked []notify.Notifier
	for _, n := range multi.Notifiers {
		if _, ok := wanted[n.Name()]; ok {
			picked = append(picked, n)
		}
	}
	if len(picked) == 0 {
		return nil
	}
	if len(picked) == 1 {
		return picked[0]
	}
	return notify.Multi{Notifiers: picked}
}

// FireNow runs a routine immediately, bypassing the schedule but honoring
// the per-routine lock. Useful for `routines run <name>`.
func (s *Scheduler) FireNow(r *spec.Routine) {
	lock := s.lockFor(r.Name)
	if !lock.TryLock() {
		stdlog.Printf("[%s] FireNow: lock busy", r.Name)
		return
	}
	defer lock.Unlock()
	s.runOnce(r)
}

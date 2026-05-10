package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ElodinLaarz/agent-routines/internal/spec"
)

// Event kinds emitted to subscribers.
type EventKind int

const (
	EventAdd EventKind = iota
	EventUpdate
	EventDelete
)

// Event is fired when the routine set changes.
type Event struct {
	Kind    EventKind
	Name    string
	Routine *spec.Routine // nil on EventDelete
	Err     error         // set when a spec failed to parse/validate
}

// LoadError records a per-file failure surfaced to `routines list`.
type LoadError struct {
	Path string
	Err  error
}

// FSStore loads routine specs from a directory and watches for changes.
type FSStore struct {
	Dir       string
	LookupEnv func(string) (string, bool) // overridable for tests

	mu       sync.RWMutex
	routines map[string]*spec.Routine // keyed by name
	pathMap  map[string]string        // path -> name
	errors   map[string]error         // path -> last load error

	subs   []chan Event
	subsMu sync.Mutex
}

// NewFSStore returns a store rooted at dir. Call Load before use.
func NewFSStore(dir string) *FSStore {
	return &FSStore{
		Dir:       dir,
		LookupEnv: os.LookupEnv,
		routines:  map[string]*spec.Routine{},
		pathMap:   map[string]string{},
		errors:    map[string]error{},
	}
}

// Load walks Dir once and parses every *.yaml / *.yml file.
func (s *FSStore) Load() error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !isYAML(e.Name()) {
			continue
		}
		s.loadFile(filepath.Join(s.Dir, e.Name()), false)
	}
	return nil
}

// Routines returns a snapshot of the currently-loaded routines.
func (s *FSStore) Routines() []*spec.Routine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*spec.Routine, 0, len(s.routines))
	for _, r := range s.routines {
		out = append(out, r)
	}
	return out
}

// LoadErrors returns per-file failures from the most recent walk.
func (s *FSStore) LoadErrors() []LoadError {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []LoadError
	for p, e := range s.errors {
		out = append(out, LoadError{Path: p, Err: e})
	}
	return out
}

// Subscribe returns a channel that receives change events. Unsubscribe by
// calling the returned cancel func.
func (s *FSStore) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	s.subsMu.Unlock()
	cancel := func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		for i, c := range s.subs {
			if c == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel
}

// Watch starts a fsnotify watcher; returns when ctx is canceled.
// Rapid changes are debounced by 200ms before re-loading the affected file.
func (s *FSStore) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(s.Dir); err != nil {
		return err
	}

	// path -> latest-event timer
	timers := map[string]*time.Timer{}
	var tmu sync.Mutex
	debounce := 200 * time.Millisecond

	schedule := func(path string, removed bool) {
		tmu.Lock()
		defer tmu.Unlock()
		if t, ok := timers[path]; ok {
			t.Stop()
		}
		// Capture a pointer to the timer we're about to install so the
		// callback only deletes its own entry. Without this check, an
		// in-flight callback whose path receives a fresh schedule() call
		// would clobber the new timer when it deletes from the map.
		var self *time.Timer
		self = time.AfterFunc(debounce, func() {
			if removed {
				s.deleteFile(path)
			} else {
				s.loadFile(path, true)
			}
			tmu.Lock()
			if timers[path] == self {
				delete(timers, path)
			}
			tmu.Unlock()
		})
		timers[path] = self
	}

	// drainTimers stops any still-pending debounce timers and clears the
	// map so callbacks already in their AfterFunc grace can no-op cleanly.
	drainTimers := func() {
		tmu.Lock()
		for k, t := range timers {
			t.Stop()
			delete(timers, k)
		}
		tmu.Unlock()
	}

	for {
		select {
		case <-ctx.Done():
			drainTimers()
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				drainTimers()
				return nil
			}
			if !isYAML(ev.Name) {
				continue
			}
			switch {
			case ev.Op&fsnotify.Remove != 0, ev.Op&fsnotify.Rename != 0:
				schedule(ev.Name, true)
			case ev.Op&(fsnotify.Create|fsnotify.Write) != 0:
				schedule(ev.Name, false)
			}
		case err, ok := <-w.Errors:
			if !ok {
				drainTimers()
				return nil
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				drainTimers()
				return err
			}
		}
	}
}

func (s *FSStore) loadFile(path string, emit bool) {
	r, err := spec.ParseFile(path, s.LookupEnv)
	if err == nil {
		err = spec.Validate(r)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		s.errors[path] = err
		// keep previous good copy if there was one
		if emit {
			s.publish(Event{Kind: EventUpdate, Name: pathToName(path), Err: err})
		}
		return
	}
	delete(s.errors, path)

	prevName := s.pathMap[path]
	if prevName != "" && prevName != r.Name {
		// renamed via spec edit: drop old entry
		delete(s.routines, prevName)
		if emit {
			s.publish(Event{Kind: EventDelete, Name: prevName})
		}
	}
	kind := EventUpdate
	if _, exists := s.routines[r.Name]; !exists {
		kind = EventAdd
	}
	s.routines[r.Name] = r
	s.pathMap[path] = r.Name
	if emit {
		s.publish(Event{Kind: kind, Name: r.Name, Routine: r})
	}
}

func (s *FSStore) deleteFile(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := s.pathMap[path]
	delete(s.pathMap, path)
	delete(s.errors, path)
	if name == "" {
		return
	}
	delete(s.routines, name)
	s.publish(Event{Kind: EventDelete, Name: name})
}

func (s *FSStore) publish(e Event) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default:
			// slow subscriber: drop
		}
	}
}

func isYAML(name string) bool {
	n := strings.ToLower(filepath.Ext(name))
	return n == ".yaml" || n == ".yml"
}

func pathToName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
}

// DefaultRoutinesDir resolves the standard config directory.
//
// XDG_CONFIG_HOME/agent-routines/routines, then ~/.routines/routines.
func DefaultRoutinesDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "agent-routines", "routines"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".routines", "routines"), nil
}

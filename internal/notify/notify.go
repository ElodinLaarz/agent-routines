// Package notify dispatches run results to configured sinks.
package notify

import (
	"context"
	"sync"
	"time"
)

// Status values used by Event.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusTimeout = "timeout"
)

// Event is the payload every sink receives.
type Event struct {
	Routine    string    `json:"routine"`
	Status     string    `json:"status"` // success | failed | timeout
	Started    time.Time `json:"started"`
	Finished   time.Time `json:"finished"`
	ExitCode   int       `json:"exit_code"`
	LogTail    string    `json:"log_tail,omitempty"`
	Error      string    `json:"error,omitempty"`
	LogPath    string    `json:"log_path,omitempty"`
}

// Notifier is the interface every sink implements.
type Notifier interface {
	Name() string
	Notify(ctx context.Context, evt Event) error
}

// Multi fans an event out to several notifiers; failures from one do not
// block others. Returns the first error after all have been attempted.
type Multi struct {
	Notifiers []Notifier
}

// Name implements Notifier.
func (m Multi) Name() string { return "multi" }

// Notify implements Notifier, fanning the event out to all child notifiers.
func (m Multi) Notify(ctx context.Context, evt Event) error {
	var (
		wg    sync.WaitGroup
		errMu sync.Mutex
		first error
	)
	for _, n := range m.Notifiers {
		wg.Add(1)
		go func(n Notifier) {
			defer wg.Done()
			if err := n.Notify(ctx, evt); err != nil {
				errMu.Lock()
				if first == nil {
					first = err
				}
				errMu.Unlock()
			}
		}(n)
	}
	wg.Wait()
	return first
}

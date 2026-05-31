package notify

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Stdout writes a one-line summary to the given writer (default os.Stderr).
type Stdout struct {
	W io.Writer
}

// Name implements Notifier.
func (s Stdout) Name() string { return "stdout" }

// Notify implements Notifier.
func (s Stdout) Notify(_ context.Context, evt Event) error {
	w := s.W
	if w == nil {
		w = os.Stderr
	}
	_, err := fmt.Fprintf(w, "[routine=%s] status=%s exit=%d duration=%s\n",
		evt.Routine, evt.Status, evt.ExitCode, evt.Finished.Sub(evt.Started))
	return err
}

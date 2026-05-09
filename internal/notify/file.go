package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// File appends one JSON object per line to Path.
type File struct {
	Path string
	mu   sync.Mutex
}

func (f *File) Name() string { return "file" }

func (f *File) Notify(_ context.Context, evt Event) error {
	if f.Path == "" {
		return fmt.Errorf("file notifier: path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fp.Close()
	if _, err := fp.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

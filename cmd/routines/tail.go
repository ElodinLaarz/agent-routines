package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// resolveLatest returns the absolute path the routine's `latest.log`
// symlink points to. Falls back to the alphabetically-newest *.log in
// dir when the symlink isn't usable (Windows without privilege, etc.).
func resolveLatest(dir, link string) (string, error) {
	target, err := os.Readlink(link)
	if err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		return target, nil
	}
	// Not a symlink (Windows, regular file, etc.) — find newest *.log.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.EqualFold(e.Name(), "latest.log") || filepath.Ext(e.Name()) != ".log" {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no .log files in %s", dir)
	}
	sort.Strings(names)
	return filepath.Join(dir, names[len(names)-1]), nil
}

func newTailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tail <name>",
		Short: "Follow the latest run log for a routine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadCfg()
			if err != nil {
				return err
			}
			dir := filepath.Join(cfg.LogDir, args[0])
			link := filepath.Join(dir, "latest.log")

			// wait for the symlink/file to exist
			deadline := time.Now().Add(60 * time.Second)
			for {
				if _, err := os.Stat(link); err == nil {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("no run started yet for %q (waited 60s)", args[0])
				}
				time.Sleep(500 * time.Millisecond)
			}

			var (
				f       *os.File
				current string
			)
			defer func() {
				if f != nil {
					_ = f.Close()
				}
			}()

			open := func() error {
				target, err := resolveLatest(dir, link)
				if err != nil {
					return err
				}
				if target == current && f != nil {
					return nil
				}
				if f != nil {
					_ = f.Close()
					f = nil
				}
				nf, err := os.Open(target)
				if err != nil {
					return err
				}
				f = nf
				current = target
				return nil
			}

			if err := open(); err != nil {
				return err
			}

			buf := make([]byte, 8192)
			for {
				n, err := f.Read(buf)
				if n > 0 {
					_, _ = cmd.OutOrStdout().Write(buf[:n])
				}
				if err == nil {
					continue
				}
				if errors.Is(err, io.EOF) {
					select {
					case <-cmd.Context().Done():
						return nil
					case <-time.After(500 * time.Millisecond):
					}
					// re-check whether latest.log now points to a newer file
					if rerr := open(); rerr != nil {
						return rerr
					}
					continue
				}
				return err
			}
		},
	}
}

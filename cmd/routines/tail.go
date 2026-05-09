package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

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
			path := filepath.Join(dir, "latest.log")

			// wait for the symlink to exist
			deadline := time.Now().Add(60 * time.Second)
			for {
				if _, err := os.Stat(path); err == nil {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("no run started yet for %q (waited 60s)", args[0])
				}
				time.Sleep(500 * time.Millisecond)
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			// Tail loop: read what's there, then poll for more.
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
					continue
				}
				return err
			}
		},
	}
}

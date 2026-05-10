package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var last int
	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Print the last N run log files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadCfg()
			if err != nil {
				return err
			}
			dir := filepath.Join(cfg.LogDir, args[0])
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no logs for %q", args[0])
				}
				return err
			}
			files := []string{}
			for _, e := range entries {
				if e.IsDir() || filepath.Ext(e.Name()) != ".log" || e.Name() == "latest.log" {
					continue
				}
				files = append(files, filepath.Join(dir, e.Name()))
			}
			// readdir is alphabetical; ISO8601 names sort newest-last
			if last <= 0 {
				last = 1
			}
			if last > len(files) {
				last = len(files)
			}
			files = files[len(files)-last:]
			for _, p := range files {
				fmt.Fprintf(cmd.OutOrStdout(), "=== %s ===\n", p)
				f, err := os.Open(p)
				if err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					continue
				}
				_, _ = io.Copy(cmd.OutOrStdout(), f)
				_ = f.Close()
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&last, "last", "n", 1, "number of recent runs to print")
	return cmd
}

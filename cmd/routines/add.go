package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ElodinLaarz/agent-routines/internal/daemoncfg"
	"github.com/ElodinLaarz/agent-routines/internal/spec"
)

func newAddCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add <file.yaml>",
		Short: "Validate a routine spec and copy it into the routines dir",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, envVals, err := loadCfg()
			if err != nil {
				return err
			}
			src := args[0]
			r, err := spec.ParseFile(src, daemoncfg.MergeLookup(envVals))
			if err != nil {
				return err
			}
			if err := spec.Validate(r); err != nil {
				return err
			}
			if warn := spec.LooksLikeSecretKeys(r); len(warn) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: env keys may contain literal secrets (use ${VAR}): %v\n", warn)
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "ok: %s\n", r.Name)
				return nil
			}
			if err := os.MkdirAll(cfg.RoutinesDir, 0o755); err != nil {
				return err
			}
			dest := filepath.Join(cfg.RoutinesDir, r.Name+".yaml")
			if err := copyFile(src, dest); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s -> %s\n", r.Name, dest)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate without writing")
	return cmd
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	// Surface Close errors so a flush failure (disk full, quota, etc.)
	// doesn't silently produce a truncated spec file.
	return out.Close()
}

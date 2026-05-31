package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build-time injected via -ldflags '-X main.version=... -X main.commit=... -X main.date=...'.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "routines %s (%s, %s)\n", version, commit, date)
		},
	}
}

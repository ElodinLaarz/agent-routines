package main

import (
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"github.com/ElodinLaarz/agent-routines/internal/daemoncfg"
	"github.com/ElodinLaarz/agent-routines/internal/spec"
	"github.com/ElodinLaarz/agent-routines/internal/store"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List loaded routines, last-run status, and next-fire ETA",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, envVals, err := loadCfg()
			if err != nil {
				return err
			}
			fs := store.NewFSStore(cfg.RoutinesDir)
			fs.LookupEnv = daemoncfg.MergeLookup(envVals)
			if err := fs.Load(); err != nil {
				return err
			}
			hist, err := store.OpenHistory(cfg.StateDB)
			if err != nil {
				return err
			}
			defer func() { _ = hist.Close() }()

			lastByName, err := hist.LastPerRoutine()
			if err != nil {
				return err
			}

			parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom |
				cron.Month | cron.Dow | cron.Descriptor)
			now := time.Now()

			rs := fs.Routines()
			sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tAGENT\tSCHEDULE\tENABLED\tLAST\tEXIT\tNEXT")
			for _, r := range rs {
				next := "-"
				if cronExpr, err := spec.ParseSchedule(r.Schedule); err == nil {
					if sched, err := parser.Parse(cronExpr); err == nil {
						next = sched.Next(now).Format(time.RFC3339)
					}
				}
				lastStatus, lastExit := "-", "-"
				if last, ok := lastByName[r.Name]; ok {
					lastStatus = last.Status
					if last.ExitCode.Valid {
						lastExit = fmt.Sprintf("%d", last.ExitCode.Int64)
					}
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%s\t%s\t%s\n",
					r.Name, r.Agent, r.Schedule, r.IsEnabled(), lastStatus, lastExit, next)
			}
			if errs := fs.LoadErrors(); len(errs) > 0 {
				_, _ = fmt.Fprintln(tw, "")
				for _, e := range errs {
					_, _ = fmt.Fprintf(tw, "BROKEN\t%s\t%v\n", e.Path, e.Err)
				}
			}
			return tw.Flush()
		},
	}
}

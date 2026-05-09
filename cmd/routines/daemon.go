package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ElodinLaarz/agent-routines/internal/daemoncfg"
	"github.com/ElodinLaarz/agent-routines/internal/scheduler"
	"github.com/ElodinLaarz/agent-routines/internal/store"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the scheduler in the foreground",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, envVals, err := loadCfg()
			if err != nil {
				return err
			}

			fs := store.NewFSStore(cfg.RoutinesDir)
			fs.LookupEnv = daemoncfg.MergeLookup(envVals)
			if err := fs.Load(); err != nil {
				return fmt.Errorf("load routines: %w", err)
			}
			for _, le := range fs.LoadErrors() {
				fmt.Fprintf(cmd.ErrOrStderr(), "spec error %s: %v\n", le.Path, le.Err)
			}

			hist, err := store.OpenHistory(cfg.StateDB)
			if err != nil {
				return fmt.Errorf("open history: %w", err)
			}
			defer hist.Close()

			sch := scheduler.New(scheduler.Config{
				Adapters:  buildAdapters(cfg),
				History:   hist,
				LogDir:    cfg.LogDir,
				Notifier:  buildNotifier(cfg),
				KeepLastN: cfg.KeepLastN,
			})

			for _, r := range fs.Routines() {
				if err := sch.AddOrReplace(r); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "schedule %s: %v\n", r.Name, err)
				}
			}

			sch.Start()
			fmt.Fprintf(cmd.OutOrStdout(), "routines daemon started: %d routine(s), watching %s\n",
				len(fs.Routines()), cfg.RoutinesDir)

			// Hot-reload loop
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			events, unsub := fs.Subscribe()
			defer unsub()

			watchErr := make(chan error, 1)
			go func() { watchErr <- fs.Watch(ctx) }()

			go func() {
				for evt := range events {
					switch evt.Kind {
					case store.EventAdd, store.EventUpdate:
						if evt.Routine == nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "spec error %s: %v\n", evt.Name, evt.Err)
							continue
						}
						if err := sch.AddOrReplace(evt.Routine); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "schedule %s: %v\n", evt.Routine.Name, err)
						} else {
							fmt.Fprintf(cmd.OutOrStdout(), "routine %s reloaded\n", evt.Routine.Name)
						}
					case store.EventDelete:
						sch.Remove(evt.Name)
						fmt.Fprintf(cmd.OutOrStdout(), "routine %s removed\n", evt.Name)
					}
				}
			}()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			select {
			case s := <-sigCh:
				fmt.Fprintf(cmd.OutOrStdout(), "got %s, draining...\n", s)
			case err := <-watchErr:
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "watch: %v\n", err)
				}
			}

			cancel()
			sch.Stop()
			return nil
		},
	}
}

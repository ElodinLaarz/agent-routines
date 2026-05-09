package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ElodinLaarz/agent-routines/internal/daemoncfg"
	"github.com/ElodinLaarz/agent-routines/internal/scheduler"
	"github.com/ElodinLaarz/agent-routines/internal/store"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name>",
		Short: "Fire one routine immediately (out-of-band, respects per-routine lock)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cfg, envVals, err := loadCfg()
			if err != nil {
				return err
			}
			fs := store.NewFSStore(cfg.RoutinesDir)
			fs.LookupEnv = daemoncfg.MergeLookup(envVals)
			if err := fs.Load(); err != nil {
				return err
			}
			for _, r := range fs.Routines() {
				if r.Name == name {
					hist, err := store.OpenHistory(cfg.StateDB)
					if err != nil {
						return err
					}
					defer hist.Close()
					sch := scheduler.New(scheduler.Config{
						Adapters:  buildAdapters(cfg),
						History:   hist,
						LogDir:    cfg.LogDir,
						Notifier:  buildNotifier(cfg),
						KeepLastN: cfg.KeepLastN,
					})
					sch.FireNow(r)
					return nil
				}
			}
			return fmt.Errorf("routine %q not found in %s", name, cfg.RoutinesDir)
		},
	}
}

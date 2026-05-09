package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ElodinLaarz/agent-routines/internal/adapter"
	"github.com/ElodinLaarz/agent-routines/internal/daemoncfg"
	"github.com/ElodinLaarz/agent-routines/internal/notify"
)

var (
	flagConfig string
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "routines",
		Short: "Cron-like scheduler for agentic CLIs (Gemini, Claude Code, shell)",
		Long: `routines runs declarative YAML routines on a schedule.

Drop spec files into ~/.routines/routines/ (or $XDG_CONFIG_HOME/agent-routines/routines)
and the daemon picks them up. Each fire invokes the configured adapter and
streams its output to a per-run log under ~/.routines/logs/.`,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&flagConfig, "config", defaultConfigPath(),
		"path to daemon config.yaml (loaded if present, else defaults are used)")

	root.AddCommand(
		newDaemonCmd(),
		newListCmd(),
		newRunCmd(),
		newAddCmd(),
		newEnableCmd(true),
		newEnableCmd(false),
		newLogsCmd(),
		newTailCmd(),
		newInstallServiceCmd(),
		newUninstallServiceCmd(),
		newVersionCmd(),
	)
	return root
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".routines", "config.yaml")
}

// loadCfg loads the daemon config + env file used by every subcommand.
func loadCfg() (*daemoncfg.Config, map[string]string, error) {
	cfg, err := daemoncfg.Load(flagConfig)
	if err != nil {
		return nil, nil, err
	}
	envVals, err := daemoncfg.LoadEnvFile(cfg.EnvFile)
	if err != nil {
		return nil, nil, fmt.Errorf("env file %s: %w", cfg.EnvFile, err)
	}
	return cfg, envVals, nil
}

// buildAdapters wires up the registry from cfg's adapter overrides.
func buildAdapters(cfg *daemoncfg.Config) *adapter.Registry {
	reg := adapter.NewRegistry()
	reg.Register(adapter.Gemini{Bin: cfg.Adapters.Gemini.Bin})
	reg.Register(adapter.Claude{Bin: cfg.Adapters.Claude.Bin, ExtraArgs: cfg.Adapters.Claude.ExtraArgs})
	reg.Register(adapter.Shell{})
	return reg
}

// buildNotifier returns a Multi notifier built from cfg's `notifiers:` list.
// If none are configured, a stdout sink is used so failures are still seen.
func buildNotifier(cfg *daemoncfg.Config) notify.Notifier {
	if len(cfg.Notifiers) == 0 {
		return notify.Stdout{}
	}
	var sinks []notify.Notifier
	for _, n := range cfg.Notifiers {
		switch n.Kind {
		case "stdout":
			sinks = append(sinks, notify.Stdout{})
		case "file":
			sinks = append(sinks, &notify.File{Path: n.Path})
		case "webhook":
			sinks = append(sinks, notify.Webhook{URL: n.URL, SlackCompat: n.SlackCompat})
		default:
			fmt.Fprintf(os.Stderr, "warning: notifier %q has unknown kind %q\n", n.Name, n.Kind)
		}
	}
	if len(sinks) == 0 {
		return notify.Stdout{}
	}
	if len(sinks) == 1 {
		return sinks[0]
	}
	return notify.Multi{Notifiers: sinks}
}

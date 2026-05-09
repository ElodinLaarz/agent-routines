package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

func newInstallServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-service",
		Short: "Install routines as a managed user service (systemd / launchd)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return installService(cmd.OutOrStdout())
		},
	}
}

func newUninstallServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall-service",
		Short: "Uninstall the previously-installed user service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return uninstallService(cmd.OutOrStdout())
		},
	}
}

func installService(out interface{}) error {
	w := out.(interface{ Write(p []byte) (int, error) })
	switch runtime.GOOS {
	case "linux":
		return installSystemd(w)
	case "darwin":
		return installLaunchd(w)
	case "windows":
		return fmt.Errorf("on Windows, run init/windows/install.ps1 (Task Scheduler)")
	}
	return fmt.Errorf("unsupported OS %q", runtime.GOOS)
}

func uninstallService(out interface{}) error {
	w := out.(interface{ Write(p []byte) (int, error) })
	switch runtime.GOOS {
	case "linux":
		return uninstallSystemd(w)
	case "darwin":
		return uninstallLaunchd(w)
	case "windows":
		return fmt.Errorf("on Windows, run init/windows/install.ps1 -Uninstall")
	}
	return fmt.Errorf("unsupported OS %q", runtime.GOOS)
}

const systemdUnitName = "agent-routines.service"

func systemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName), nil
}

func installSystemd(out interface{ Write(p []byte) (int, error) }) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.Abs(exe)
	unit := fmt.Sprintf(`[Unit]
Description=agent-routines daemon
After=network-online.target

[Service]
ExecStart=%s daemon
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, exe)
	dst, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(unit), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", systemdUnitName},
		{"--user", "start", systemdUnitName},
	} {
		c := exec.Command("systemctl", args...)
		c.Stdout, c.Stderr = osStdout(), osStderr()
		if err := c.Run(); err != nil {
			return fmt.Errorf("systemctl %v: %w", args, err)
		}
	}
	fmt.Fprintf(out, "installed %s\n", dst)
	return nil
}

func uninstallSystemd(out interface{ Write(p []byte) (int, error) }) error {
	for _, args := range [][]string{
		{"--user", "stop", systemdUnitName},
		{"--user", "disable", systemdUnitName},
	} {
		c := exec.Command("systemctl", args...)
		c.Stdout, c.Stderr = osStdout(), osStderr()
		_ = c.Run() // tolerate failure (already stopped, etc.)
	}
	dst, err := systemdUnitPath()
	if err != nil {
		return err
	}
	_ = os.Remove(dst)
	c := exec.Command("systemctl", "--user", "daemon-reload")
	_ = c.Run()
	fmt.Fprintf(out, "removed %s\n", dst)
	return nil
}

const launchdLabel = "com.elodinlaarz.agent-routines"

func launchdPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func installLaunchd(out interface{ Write(p []byte) (int, error) }) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.Abs(exe)
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, launchdLabel, exe,
		filepath.Join(homeOr(""), ".routines", "daemon.log"),
		filepath.Join(homeOr(""), ".routines", "daemon.log"))
	dst, err := launchdPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(plist), 0o644); err != nil {
		return err
	}
	c := exec.Command("launchctl", "load", "-w", dst)
	c.Stdout, c.Stderr = osStdout(), osStderr()
	if err := c.Run(); err != nil {
		return err
	}
	fmt.Fprintf(out, "installed %s\n", dst)
	return nil
}

func uninstallLaunchd(out interface{ Write(p []byte) (int, error) }) error {
	dst, err := launchdPath()
	if err != nil {
		return err
	}
	c := exec.Command("launchctl", "unload", "-w", dst)
	_ = c.Run()
	_ = os.Remove(dst)
	fmt.Fprintf(out, "removed %s\n", dst)
	return nil
}

func homeOr(fallback string) string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return fallback
}

func osStdout() *os.File { return os.Stdout }
func osStderr() *os.File { return os.Stderr }

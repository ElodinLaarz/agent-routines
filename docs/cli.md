# CLI reference

Every command honors `--config <path>` to override the daemon config
file (default `~/.routines/config.yaml`).

## `routines daemon`

Runs the scheduler in the foreground. Watches the routines directory
with fsnotify; spec edits take effect without restarting.

Signals: SIGINT/SIGTERM trigger graceful shutdown — in-flight runs are
allowed up to `grace_period` (default 30s) to finish.

## `routines list`

Tabular view of every loaded routine: agent, schedule, last status,
last exit code, next fire time. Broken specs are listed at the bottom
with the load error.

## `routines run <name>`

Fires one routine immediately. Honors the same per-routine lock the
daemon does, so this is safe to call while the daemon is running. If
the daemon is down, runs inline.

## `routines add <file.yaml> [--dry-run]`

Validates a spec, then copies it to `<routines_dir>/<name>.yaml`. With
`--dry-run` it only validates.

## `routines enable <name>` / `routines disable <name>`

Edits the spec on disk to flip the `enabled:` field. The daemon
notices via fsnotify and updates the schedule.

## `routines logs <name> [--last N]`

Cats the last N run log files for a routine. `latest.log` symlinks the
most recent.

## `routines tail <name>`

Follows `<log_dir>/<name>/latest.log`. Waits up to 60s for the symlink
if the routine has not fired yet.

## `routines install-service` / `uninstall-service`

Drops a systemd `--user` unit (Linux) or launchd plist (macOS). On
Windows, run `init/windows/install.ps1` directly.

## `routines version`

Prints version, commit, and build date.

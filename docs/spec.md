# Routine spec

A routine is a single YAML file under `~/.routines/routines/`. One
routine per file. Filename does not have to match `name`, but `add`
will use `name` to choose the destination.

## Fields

| Field        | Type                | Required                      | Notes |
|--------------|---------------------|-------------------------------|-------|
| `name`       | string              | yes                           | Slug-safe (alnum, dash, dot, underscore). Unique across routines. |
| `agent`      | enum                | yes                           | `gemini`, `claude`, `shell`. |
| `schedule`   | string              | yes                           | 5-field cron, `every Nm/h/d`, `daily HH:MM`, or `hourly`. |
| `prompt`     | string              | required for `gemini`/`claude` | Multi-line OK. `${VAR}` expansion. |
| `command`    | list of strings     | required for `shell`           | Argv. `prompt` (if set) is piped to stdin. |
| `workdir`    | string              | no                            | `~` and `${VAR}` expanded. Defaults to daemon cwd. |
| `timeout`    | duration            | no                            | Default 10m. e.g. `30s`, `5m`, `2h`. |
| `on_failure` | enum                | no                            | `retry` \| `skip` \| `alert`. Default `skip`. |
| `retries`    | int                 | no                            | Used when `on_failure: retry`. |
| `backoff`    | duration            | no                            | Sleep between retries. Default 30s. |
| `outputs`    | list                | no                            | `{ log: <path> }` or `{ notifier: <name> }`. |
| `env`        | map[string]string   | no                            | `${VAR}` expansion from env-file or process env. |
| `env_file`   | string              | no                            | Per-routine env-file override. |
| `enabled`    | bool                | no                            | Default `true`. |
| `once`       | bool                | no                            | Fire once, then delete the spec file. |
| `worktree`   | object              | no                            | Run each fire inside a fresh git worktree. See below. |

## Schedule grammar

- 5-field cron: `*/5 * * * *`
- `every <duration>`: `every 30s`, `every 5m`, `every 2h`, `every 1d`,
  `every 1d12h`. Standard Go duration syntax plus the `d` (day) suffix.
- `daily HH:MM`: `daily 09:30`
- `hourly`: shorthand for `0 * * * *`

## ${VAR} expansion

Values in `env`, `prompt`, `workdir`, and `command` honor `${VAR}`. The
daemon resolves them in order:

1. Daemon env-file (`~/.routines/env`)
2. Per-routine `env_file:` (overrides 1)
3. Process environment

Missing vars are left literal — fail loudly via the prompt rather than
silently substitute.

## Secrets

- Never put a literal key in `env:`. Use `${GEMINI_API_KEY}` and put
  the value in `~/.routines/env`.
- The validator emits a heuristic warning when a key name contains
  KEY/TOKEN/SECRET/PASSWORD/API and the value is long opaque.

## Worktree mode

When `worktree:` is present, the daemon creates a fresh `git worktree`
at the start of every fire, runs the routine inside it, and removes
the worktree (and its branch) when the routine exits. Each fire
therefore gets an isolated copy of the repo — concurrent runs and
runs that mutate files cannot stomp each other.

| Sub-field        | Notes |
|------------------|-------|
| `branch_prefix`  | Prepended to the auto-generated branch name. Default `routines/`. |
| `path`           | Worktree directory, relative to repo root. Default `.worktrees/<routine-name>-<run-id>`. |
| `post_create`    | Optional shell command run inside the worktree before the agent fires (e.g. `npm install`, `cargo build`). |

Example:

```yaml
name: refactor-bot
agent: claude
schedule: "every 1h"
workdir: ~/repos/foo
worktree:
  post_create: "npm ci"
prompt: |
  Look for cleanup opportunities. Open a PR if you find any.
```

## One-shot routines

Setting `once: true` makes the routine fire one time and removes the
spec file after a successful run. Useful for deferred work (`run this
once at 3pm and forget`).

```yaml
name: at-3pm
agent: shell
schedule: "0 15 * * *"
once: true
command: ["bash", "-lc", "/usr/local/bin/some-batch-job"]
```

## Example

```yaml
name: morning-triage
agent: gemini
schedule: "0 9 * * *"
workdir: ~/repos/foo
prompt: |
  Review overnight PRs. Summarize blockers in 5 bullets.
timeout: 10m
on_failure: alert
env:
  GEMINI_API_KEY: ${GEMINI_API_KEY}
```

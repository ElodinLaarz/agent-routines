# Shell adapter

The escape-hatch — runs an arbitrary command. Covers any agent CLI
that does not yet have a first-class adapter.

## Invocation

The first element of `command:` is the executable; the rest are argv.
If `prompt:` is set, it is piped to the child's stdin.

```yaml
name: log-scan
agent: shell
schedule: "every 15m"
command: ["bash", "-lc", "journalctl -u myservice --since='15 minutes ago' | grep ERROR || true"]
timeout: 1m
```

## Streaming

stdout/stderr stream to the per-run log file in real time. Exit code
becomes the run's exit code. Timeout kills the entire process tree
(POSIX `setpgid` / Windows `CREATE_NEW_PROCESS_GROUP` + `taskkill /T`).

## When to use this vs. a dedicated adapter

- One-off shell pipelines: shell adapter.
- Anything you would invoke as `agent --auto --some-flags`: write a
  dedicated adapter so flags, env, and exit-code semantics live in
  one place.

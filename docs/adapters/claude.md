# Claude adapter

Wraps the Claude Code CLI in headless mode.

## Invocation

```
claude -p "<prompt>"
```

`extra_args` from daemon config are appended verbatim. For example, to
get structured output:

```yaml
adapters:
  claude:
    bin: claude
    extra_args: ["--output-format", "stream-json"]
```

## Auth

Either:

- `claude login` once interactively, then the daemon inherits the
  session, OR
- set `ANTHROPIC_API_KEY` in `~/.routines/env`.

```yaml
env:
  ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
```

## Tool permissions

Headless `claude -p` runs in a constrained mode by default. If your
routine needs to edit files or run shell commands, configure the
appropriate `--allowed-tools` flags via `extra_args`.

## Gotchas

- Long prompts: pass via `-p`. Stdin is not used by this adapter.
- The headless flag has been `-p` for several versions but verify
  against the binary you have installed.

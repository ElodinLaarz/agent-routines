# Gemini adapter

Wraps the `gemini` CLI in non-interactive mode.

## Invocation

```
gemini -p "<prompt>"
```

stdout/stderr stream to the per-run log file. ExitCode propagates.

## Auth

Requires `GEMINI_API_KEY` in the environment. Put it in
`~/.routines/env` and reference it in the routine as
`${GEMINI_API_KEY}`.

```yaml
env:
  GEMINI_API_KEY: ${GEMINI_API_KEY}
```

## Binary path

Defaults to whatever `gemini` resolves to on `PATH`. Override in
daemon config:

```yaml
adapters:
  gemini:
    bin: /usr/local/bin/gemini
```

## Gotchas

- The CLI's flag for non-interactive mode has changed across versions.
  Verify against the binary you have installed; `routines version`
  doesn't translate to a Gemini CLI version.
- The prompt argument is passed via `-p`, not stdin, to avoid quoting
  surprises.

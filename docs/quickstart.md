# Quickstart

Goal: from cold install to a routine firing in under five minutes.

## 1. Install

```bash
go install github.com/ElodinLaarz/agent-routines/cmd/routines@latest
routines version
```

(or grab a pre-built binary from the latest release once the pipeline is up)

## 2. Pick or write a spec

Copy one of the examples:

```bash
mkdir -p ~/.routines/routines
cp examples/routines/log-scan.yaml ~/.routines/routines/
```

Or write your own — see [spec.md](spec.md) for the schema.

## 3. Provide secrets out-of-band

Put API keys in `~/.routines/env`, never in YAML:

```bash
cat > ~/.routines/env <<'EOF'
GEMINI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-...
EOF
chmod 600 ~/.routines/env
```

In your routine, reference them as `${GEMINI_API_KEY}`.

## 4. Validate

```bash
routines list
```

If a spec is broken, the bottom of the table shows a `BROKEN` row with
the file path and error. Fix and re-run.

## 5. Smoke-test out-of-band

```bash
routines run log-scan
routines logs log-scan -n 1
```

## 6. Run as a service

```bash
# Linux (systemd --user)
routines install-service
systemctl --user status agent-routines

# macOS (launchd user agent)
routines install-service
launchctl list | grep agent-routines

# Windows
powershell -ExecutionPolicy Bypass -File init\windows\install.ps1
Get-ScheduledTask -TaskName agent-routines
```

To stop / remove:

```bash
routines uninstall-service             # Linux/macOS
powershell -File init\windows\install.ps1 -Uninstall   # Windows
```

## What's next

- Add a notifier in `~/.routines/config.yaml` so failures actually
  reach somebody. See [config example](../examples/config.yaml).
- Read [adapters/gemini.md](adapters/gemini.md),
  [adapters/claude.md](adapters/claude.md), or
  [adapters/shell.md](adapters/shell.md) for adapter-specific notes.

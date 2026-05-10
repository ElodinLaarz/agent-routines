package spec

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var slugRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidationError carries a field path with the failure reason.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Msg
	}
	return e.Field + ": " + e.Msg
}

func newErr(field, format string, args ...any) error {
	return &ValidationError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

// Validate enforces the routine schema.
func Validate(r *Routine) error {
	if r == nil {
		return newErr("", "nil routine")
	}
	if r.Name == "" {
		return newErr("name", "required")
	}
	if !slugRE.MatchString(r.Name) {
		return newErr("name", "must be slug-safe (alnum, dash, dot, underscore)")
	}
	if !ValidAgents[r.Agent] {
		return newErr("agent", "must be one of gemini, claude, shell (got %q)", r.Agent)
	}
	if r.Schedule == "" {
		return newErr("schedule", "required")
	}
	if _, err := ParseSchedule(r.Schedule); err != nil {
		return newErr("schedule", "%v", err)
	}
	switch r.Agent {
	case "shell":
		if len(r.Command) == 0 {
			return newErr("command", "required for agent=shell")
		}
	default:
		if r.Prompt == "" {
			return newErr("prompt", "required for agent=%s", r.Agent)
		}
	}
	if !ValidOnFailure[r.OnFailure] {
		return newErr("on_failure", "must be retry|skip|alert (got %q)", r.OnFailure)
	}
	if r.OnFailure == "retry" && r.Retries < 0 {
		return newErr("retries", "must be >= 0")
	}
	if r.Timeout < 0 {
		return newErr("timeout", "must be >= 0")
	}
	for k := range r.Env {
		if k == "" {
			return newErr("env", "empty key")
		}
	}
	if r.Worktree != nil && r.Workdir == "" {
		return newErr("worktree", "requires `workdir:` to point at a git repository")
	}
	// Note: secret-looking env values are surfaced via LooksLikeSecretKeys
	// for callers to warn on; we never fail validation on heuristics alone.
	return nil
}

// LooksLikeSecretKeys is exposed so the daemon can emit warnings on load.
func LooksLikeSecretKeys(r *Routine) []string {
	var keys []string
	for k, v := range r.Env {
		if looksLikeSecret(k, v) {
			keys = append(keys, k)
		}
	}
	return keys
}

func looksLikeSecret(k, v string) bool {
	if v == "" {
		return false
	}
	upper := strings.ToUpper(k)
	for _, kw := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "API"} {
		if strings.Contains(upper, kw) {
			// If the value still looks like a placeholder/template, assume OK.
			if strings.Contains(v, "${") {
				return false
			}
			// Bare empty string is fine; long opaque value is suspicious.
			if len(v) >= 20 && !strings.ContainsAny(v, " /\\") {
				return true
			}
		}
	}
	return false
}

// dayRE matches the leading `<int>d` segment of a duration expression.
var dayRE = regexp.MustCompile(`^(\d+)d`)

// expandDays rewrites a leading `Nd` into `(N*24)h` so time.ParseDuration
// accepts it. Returns the rewritten string and true if a substitution
// occurred.
func expandDays(s string) (string, bool) {
	m := dayRE.FindStringSubmatch(s)
	if m == nil {
		return s, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return s, false
	}
	return fmt.Sprintf("%dh", n*24) + s[len(m[0]):], true
}

// ParseSchedule normalizes a schedule string into a cron expression that
// robfig/cron's standard parser accepts. It accepts:
//
//	"every 30s" / "every 5m" / "every 2h" / "every 1d"
//	"daily HH:MM"
//	"hourly"
//	5-field cron (passed through)
func ParseSchedule(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty schedule")
	}
	lower := strings.ToLower(s)
	switch {
	case lower == "hourly":
		return "0 * * * *", nil
	case strings.HasPrefix(lower, "daily "):
		hhmm := strings.TrimSpace(lower[len("daily "):])
		parts := strings.Split(hhmm, ":")
		if len(parts) != 2 {
			return "", fmt.Errorf("daily HH:MM expected, got %q", s)
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return "", fmt.Errorf("invalid HH:MM in %q", s)
		}
		return fmt.Sprintf("%d %d * * *", m, h), nil
	case strings.HasPrefix(lower, "every "):
		arg := strings.TrimSpace(lower[len("every "):])
		// time.ParseDuration does not understand `d`; expand `Nd` to N*24h
		// and any compound like `1d12h` to its h-equivalent.
		if expanded, ok := expandDays(arg); ok {
			arg = expanded
		}
		d, err := time.ParseDuration(arg)
		if err != nil {
			return "", fmt.Errorf("every <duration>: %w", err)
		}
		if d <= 0 {
			return "", fmt.Errorf("every <duration> must be > 0")
		}
		return "@every " + d.String(), nil
	default:
		// Trust 5-field cron through robfig parser later.
		if fields := strings.Fields(s); len(fields) != 5 {
			return "", fmt.Errorf("expected 5-field cron or 'every N' / 'daily HH:MM' / 'hourly', got %q", s)
		}
		return s, nil
	}
}

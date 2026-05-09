package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseMinimal(t *testing.T) {
	y := []byte(`
name: hello
agent: shell
schedule: "every 1m"
command: ["echo", "hi"]
`)
	r, err := Parse(y, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(r); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if r.Timeout != 10*time.Minute {
		t.Errorf("default timeout: got %v", r.Timeout)
	}
	if !r.IsEnabled() {
		t.Error("should be enabled by default")
	}
}

func TestEnvExpand(t *testing.T) {
	env := map[string]string{"FOO": "bar"}
	look := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	y := []byte(`
name: t
agent: shell
schedule: hourly
command: ["echo", "${FOO}"]
env:
  X: ${FOO}-baz
`)
	r, err := Parse(y, look)
	if err != nil {
		t.Fatal(err)
	}
	if r.Command[1] != "bar" {
		t.Errorf("command not expanded: %v", r.Command)
	}
	if r.Env["X"] != "bar-baz" {
		t.Errorf("env not expanded: %v", r.Env)
	}
}

func TestValidateRejectsBadAgent(t *testing.T) {
	r := &Routine{Name: "x", Agent: "bogus", Schedule: "hourly", Prompt: "hi"}
	err := Validate(r)
	if err == nil || !strings.Contains(err.Error(), "agent") {
		t.Errorf("expected agent error, got %v", err)
	}
}

func TestValidateShellRequiresCommand(t *testing.T) {
	r := &Routine{Name: "x", Agent: "shell", Schedule: "hourly"}
	err := Validate(r)
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("expected command error, got %v", err)
	}
}

func TestValidateAgentRequiresPrompt(t *testing.T) {
	r := &Routine{Name: "x", Agent: "gemini", Schedule: "hourly"}
	err := Validate(r)
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Errorf("expected prompt error, got %v", err)
	}
}

func TestParseSchedule(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"hourly", "0 * * * *", false},
		{"daily 09:30", "30 9 * * *", false},
		{"every 30s", "@every 30s", false},
		{"every 5m", "@every 5m0s", false},
		{"every 1d", "@every 24h0m0s", false},
		{"every 2d", "@every 48h0m0s", false},
		{"every 1d12h", "@every 36h0m0s", false},
		{"*/5 * * * *", "*/5 * * * *", false},
		{"daily 25:00", "", true},
		{"every -1m", "", true},
		{"junk", "", true},
	}
	for _, c := range cases {
		got, err := ParseSchedule(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestEnvFileOverridesLookup(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("FOO=from-file\nBAR=baz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	look := func(k string) (string, bool) {
		if k == "FOO" {
			return "from-lookup", true
		}
		return "", false
	}
	y := []byte(`
name: t
agent: shell
schedule: hourly
command: ["echo", "${FOO}-${BAR}"]
env_file: ` + envPath + `
`)
	r, err := Parse(y, look)
	if err != nil {
		t.Fatal(err)
	}
	// per-routine env_file wins over caller lookup
	if r.Command[1] != "from-file-baz" {
		t.Errorf("got %q", r.Command[1])
	}
}

func TestSecretHeuristic(t *testing.T) {
	r := &Routine{Env: map[string]string{
		"GEMINI_API_KEY": "sk-aaaaaaaaaaaaaaaaaaaaaaaa",
		"NON_SECRET":     "value",
	}}
	keys := LooksLikeSecretKeys(r)
	if len(keys) != 1 || keys[0] != "GEMINI_API_KEY" {
		t.Errorf("got %v", keys)
	}
}

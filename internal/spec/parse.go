package spec

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultTimeout = 10 * time.Minute
)

// ${VAR} or $VAR (POSIX-style) but we only honor ${VAR} for clarity.
var envRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ParseFile reads a routine YAML file and returns a parsed, defaulted, and
// env-expanded Routine. It does NOT validate; call Validate separately.
func ParseFile(path string, lookupEnv func(string) (string, bool)) (*Routine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := Parse(data, lookupEnv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r.SourcePath = path
	return r, nil
}

// Parse decodes a routine spec from raw YAML bytes.
func Parse(data []byte, lookupEnv func(string) (string, bool)) (*Routine, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	var r Routine
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("yaml decode: %w", err)
	}

	// expand ${VAR} in env values, prompt, workdir, command args
	expand := func(s string) string {
		return envRE.ReplaceAllStringFunc(s, func(m string) string {
			name := m[2 : len(m)-1]
			if v, ok := lookupEnv(name); ok {
				return v
			}
			return m
		})
	}
	for k, v := range r.Env {
		r.Env[k] = expand(v)
	}
	r.Prompt = expand(r.Prompt)
	r.Workdir = expand(r.Workdir)
	for i := range r.Command {
		r.Command[i] = expand(r.Command[i])
	}

	if r.Timeout == 0 {
		r.Timeout = defaultTimeout
	}
	if r.Workdir != "" {
		r.Workdir = expandHome(r.Workdir)
	}
	return &r, nil
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return home + p[1:]
	}
	return p
}

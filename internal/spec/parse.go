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
//
// Order of resolution for ${VAR} expansion (highest priority first):
//
//  1. Per-routine env_file (if `env_file:` is set)
//  2. lookupEnv (caller-provided — typically daemon env-file + os.Environ)
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

	// Resolve r.EnvFile first so its values are available during the
	// expansion of every other field. The env_file path itself may
	// reference ${VAR}s from the parent lookup.
	var perRoutineVals map[string]string
	if r.EnvFile != "" {
		expandedPath := envRE.ReplaceAllStringFunc(r.EnvFile, func(m string) string {
			name := m[2 : len(m)-1]
			if v, ok := lookupEnv(name); ok {
				return v
			}
			return m
		})
		expandedPath = expandHome(expandedPath)
		r.EnvFile = expandedPath
		vals, err := loadDotenv(expandedPath)
		if err != nil {
			return nil, fmt.Errorf("env_file %s: %w", expandedPath, err)
		}
		perRoutineVals = vals
	}

	expand := func(s string) string {
		return envRE.ReplaceAllStringFunc(s, func(m string) string {
			name := m[2 : len(m)-1]
			if v, ok := perRoutineVals[name]; ok {
				return v
			}
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

// loadDotenv parses a KEY=VALUE file. # comments and blank lines OK.
// Surrounding single/double quotes on values are stripped. Missing files
// are silently treated as empty so optional env_files do not break specs.
func loadDotenv(path string) (map[string]string, error) {
	m := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", i+1)
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"'`)
		m[k] = v
	}
	return m, nil
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

// Package daemoncfg loads ~/.routines/config.yaml and the env file used
// for ${VAR} expansion in routine specs.
package daemoncfg

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the daemon-wide settings file.
type Config struct {
	RoutinesDir string         `yaml:"routines_dir,omitempty"`
	LogDir      string         `yaml:"log_dir,omitempty"`
	StateDB     string         `yaml:"state_db,omitempty"`
	EnvFile     string         `yaml:"env_file,omitempty"`
	KeepLastN   int            `yaml:"keep_last_n,omitempty"`
	Notifiers   []NotifierSpec `yaml:"notifiers,omitempty"`
	Adapters    AdapterSpec    `yaml:"adapters,omitempty"`
}

// NotifierSpec is one daemon-level sink declaration.
type NotifierSpec struct {
	Name        string `yaml:"name"`
	Kind        string `yaml:"kind"`           // stdout | file | webhook
	Path        string `yaml:"path,omitempty"` // file
	URL         string `yaml:"url,omitempty"`  // webhook
	SlackCompat bool   `yaml:"slack_compat,omitempty"`
}

// AdapterSpec lets the user override binary paths.
type AdapterSpec struct {
	Gemini struct {
		Bin string `yaml:"bin,omitempty"`
	} `yaml:"gemini,omitempty"`
	Claude struct {
		Bin       string   `yaml:"bin,omitempty"`
		ExtraArgs []string `yaml:"extra_args,omitempty"`
	} `yaml:"claude,omitempty"`
}

// Defaults returns a Config seeded with the standard XDG paths.
func Defaults() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".routines")
	return &Config{
		RoutinesDir: filepath.Join(root, "routines"),
		LogDir:      filepath.Join(root, "logs"),
		StateDB:     filepath.Join(root, "state.db"),
		EnvFile:     filepath.Join(root, "env"),
		KeepLastN:   50,
	}, nil
}

// Load reads the daemon config (or returns Defaults when absent).
func Load(path string) (*Config, error) {
	d, err := Defaults()
	if err != nil {
		return nil, err
	}
	if path == "" {
		return d, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// LoadEnvFile parses a dotenv file and returns its values. Missing files
// are silently treated as empty.
//
// Format: KEY=value, lines starting with # are comments, surrounding
// quotes on the value are stripped. No shell-style expansion.
func LoadEnvFile(path string) (map[string]string, error) {
	m := map[string]string{}
	if path == "" {
		return m, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNo)
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"'`)
		m[k] = v
	}
	return m, sc.Err()
}

// MergeLookup returns a function that walks daemon-env -> file-env ->
// process-env, redacting nothing — caller must avoid logging values.
func MergeLookup(envFileVals map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		if v, ok := envFileVals[k]; ok {
			return v, true
		}
		return os.LookupEnv(k)
	}
}

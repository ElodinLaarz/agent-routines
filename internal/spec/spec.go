// Package spec defines the routine YAML schema and its parser/validator.
package spec

import "time"

// Routine is one declarative job.
type Routine struct {
	Name       string            `yaml:"name"`
	Agent      string            `yaml:"agent"`
	Schedule   string            `yaml:"schedule"`
	Prompt     string            `yaml:"prompt,omitempty"`
	Command    []string          `yaml:"command,omitempty"`
	Workdir    string            `yaml:"workdir,omitempty"`
	Timeout    time.Duration     `yaml:"timeout,omitempty"`
	OnFailure  string            `yaml:"on_failure,omitempty"` // retry | skip | alert
	Retries    int               `yaml:"retries,omitempty"`
	Backoff    time.Duration     `yaml:"backoff,omitempty"`
	Outputs    []Output          `yaml:"outputs,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	EnvFile    string            `yaml:"env_file,omitempty"`
	Enabled    *bool             `yaml:"enabled,omitempty"`

	// SourcePath is the file the routine was loaded from (set by store).
	SourcePath string `yaml:"-"`
}

// Output is one notifier/sink reference for a routine.
type Output struct {
	Log      string `yaml:"log,omitempty"`      // path template
	Notifier string `yaml:"notifier,omitempty"` // name from daemon config
}

// IsEnabled returns true unless explicitly disabled.
func (r *Routine) IsEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// Valid agent values for v1.
var ValidAgents = map[string]bool{
	"gemini": true,
	"claude": true,
	"shell":  true,
}

// Valid on_failure values.
var ValidOnFailure = map[string]bool{
	"":       true, // default skip
	"retry":  true,
	"skip":   true,
	"alert":  true,
}

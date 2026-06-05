package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogsMissingNameErrorIsHelpful(t *testing.T) {
	cmd := newRootCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"logs"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{
		"missing routine name",
		"Usage: routines logs <name> [-n N]",
		"routines list",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "accepts 1 arg") {
		t.Fatalf("error still contains generic cobra arity message: %q", msg)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected cobra error output to be silenced, got %q", stderr.String())
	}
}

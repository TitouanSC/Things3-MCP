package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsUnavailableModes(t *testing.T) {
	env := func(string) string { return "" }
	for _, args := range [][]string{{"-transport", "http"}, {"-backend", "cloud"}, {"-unknown"}} {
		var stderr bytes.Buffer
		if err := run(context.Background(), args, env, &stderr); err == nil {
			t.Errorf("run(%q) succeeded, want an error", args)
		}
	}
}

func TestRunVersion(t *testing.T) {
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"-version"}, func(string) string { return "" }, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "things-mcp dev") {
		t.Errorf("output = %q", stderr.String())
	}
}

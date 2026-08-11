package cli

import (
	"reflect"
	"testing"
)

func TestShouldPassthrough(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantArgs []string
		wantOK   bool
	}{
		{"no args", []string{"sb"}, nil, false},
		{"help flag", []string{"sb", "--help"}, nil, false},
		{"short flag", []string{"sb", "-v"}, nil, false},
		{"known command: create", []string{"sb", "create", "-m", "x"}, nil, false},
		{"known command: log", []string{"sb", "log"}, nil, false},
		{"known alias: co", []string{"sb", "co"}, nil, false},
		{"cobra builtin: help", []string{"sb", "help", "modify"}, nil, false},
		{"passthrough: status", []string{"sb", "status"}, []string{"status"}, true},
		{"passthrough: rev-parse HEAD", []string{"sb", "rev-parse", "HEAD"}, []string{"rev-parse", "HEAD"}, true},
		{"passthrough: unknown-word", []string{"sb", "not-a-real-command"}, []string{"not-a-real-command"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotArgs, gotOK := ShouldPassthrough(tc.args)
			if gotOK != tc.wantOK {
				t.Fatalf("ok: got %v want %v", gotOK, tc.wantOK)
			}
			if tc.wantOK && !reflect.DeepEqual(gotArgs, tc.wantArgs) {
				t.Fatalf("args: got %v want %v", gotArgs, tc.wantArgs)
			}
		})
	}
}

func TestIsKnownCommand(t *testing.T) {
	// A sampling covering own commands, aliases, and cobra builtins.
	for _, name := range []string{"create", "modify", "restack", "move", "sync", "continue", "submit", "checkout", "co", "up", "down", "top", "bottom", "track", "untrack", "log", "help", "completion"} {
		if !IsKnownCommand(name) {
			t.Errorf("expected %q to be a known command", name)
		}
	}
	for _, name := range []string{"status", "push", "pull", "rev-parse", "diff", "add", ""} {
		if IsKnownCommand(name) {
			t.Errorf("expected %q to be unknown", name)
		}
	}
}

// Package ghx wraps the GitHub `gh` CLI.
package ghx

import (
	"errors"
	"os/exec"
)

var (
	ErrNotFound = errors.New(
		"gh is required but not found on PATH — install from https://cli.github.com or run `nix profile install nixpkgs#gh`",
	)
	ErrNotAuthed = errors.New(
		"gh is installed but not authenticated — run `gh auth login` first",
	)
)

// Preflight verifies gh is installed and authenticated. Callers should invoke
// this only from commands that actually need the network (submit, sync), so
// local-only commands still work without a gh install.
func Preflight() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return ErrNotFound
	}
	if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		return ErrNotAuthed
	}
	return nil
}

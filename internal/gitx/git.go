// Package gitx wraps the `git` CLI. It never uses libgit2 and never talks to a
// remote directly — every remote op goes through `gh` or through git's own
// remote handling.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotFound is returned by Preflight when the git binary is missing.
var ErrNotFound = errors.New(
	"git is required but not found on PATH — install from https://git-scm.com or run `nix profile install nixpkgs#git`",
)

// Preflight verifies git is installed and returns a helpful error if not.
func Preflight() error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrNotFound
	}
	if _, err := run("git", "--version"); err != nil {
		return fmt.Errorf("git is installed but not working: %w", err)
	}
	return nil
}

func run(name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", strings.Join(append([]string{name}, args...), " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func RepoRoot() (string, error) {
	return run("git", "rev-parse", "--show-toplevel")
}

func CurrentBranch() (string, error) {
	return run("git", "symbolic-ref", "--short", "HEAD")
}

func CheckoutNew(branch string) error {
	_, err := run("git", "checkout", "-b", branch)
	return err
}

func Checkout(branch string) error {
	_, err := run("git", "checkout", branch)
	return err
}

func Commit(message string) error {
	_, err := run("git", "commit", "-m", message)
	return err
}

// HasStagedChanges reports whether the index differs from HEAD.
func HasStagedChanges() (bool, error) {
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	// Exit code 1 means "there are differences" — that's what we want.
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

// StagedFileCount returns the number of files staged for the next commit.
func StagedFileCount() (int, error) {
	out, err := run("git", "diff", "--cached", "--name-only")
	if err != nil {
		return 0, err
	}
	if out == "" {
		return 0, nil
	}
	return strings.Count(out, "\n") + 1, nil
}

// GetConfig reads a git config value. Returns an empty string (no error) if the
// key is unset — git exits 1 in that case, which we treat as absent.
func GetConfig(key string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return strings.TrimSpace(stdout.String()), nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return "", nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = err.Error()
	}
	return "", fmt.Errorf("git config --get %s: %s", key, msg)
}

func SetConfig(key, value string) error {
	_, err := run("git", "config", key, value)
	return err
}

// ListBranches returns the short names of all local branches.
func ListBranches() ([]string, error) {
	out, err := run("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

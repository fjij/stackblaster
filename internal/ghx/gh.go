// Package ghx wraps the GitHub `gh` CLI.
package ghx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

func run(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("gh", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// PRInfo is the subset of gh pr view output we care about.
type PRInfo struct {
	Number     int    `json:"number"`
	State      string `json:"state"`
	Draft      bool   `json:"isDraft"`
	BaseRef    string `json:"baseRefName"`
	HeadRef    string `json:"headRefName"`
	URL        string `json:"url"`
	Title      string `json:"title"`
}

// PRForBranch returns the PR associated with the given head branch, or nil if
// none exists.
func PRForBranch(branch string) (*PRInfo, error) {
	out, err := run(
		"pr", "view", branch,
		"--json", "number,state,isDraft,baseRefName,headRefName,url,title",
	)
	if err != nil {
		// gh returns non-zero when no PR exists. Distinguish that case.
		if strings.Contains(err.Error(), "no pull requests found") {
			return nil, nil
		}
		return nil, err
	}
	var pr PRInfo
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// CreatePROpts controls PR creation.
type CreatePROpts struct {
	Head  string
	Base  string
	Title string
	Body  string
	Draft bool
}

// CreatePR opens a PR and returns the URL.
func CreatePR(opts CreatePROpts) (string, error) {
	args := []string{
		"pr", "create",
		"--head", opts.Head,
		"--base", opts.Base,
		"--title", opts.Title,
		"--body", opts.Body,
	}
	if opts.Draft {
		args = append(args, "--draft")
	}
	out, err := run(args...)
	if err != nil {
		return "", err
	}
	// gh pr create prints the URL as the last line.
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "https://") {
			return strings.TrimSpace(lines[i]), nil
		}
	}
	return out, nil
}

// SetPRBase changes an existing PR's base branch.
func SetPRBase(branch, base string) error {
	_, err := run("pr", "edit", branch, "--base", base)
	return err
}

// Passthrough runs `gh` with stdio wired to the terminal — useful for auth or
// diagnostic commands we don't parse.
func Passthrough(args ...string) error {
	cmd := exec.Command("gh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

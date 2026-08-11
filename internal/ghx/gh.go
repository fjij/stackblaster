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

// --- GitHub Stacked PRs REST API --------------------------------------------
//
// See https://docs.github.com/en/rest/pulls/stacks
//
// GitHub's stacked-PR feature exposes a set of REST endpoints under
// /repos/{owner}/{repo}/stacks. Creating a "stack" server-side is what
// triggers the "Stacked pull request" banner in the UI; just having one PR's
// base be another PR's head is not enough. sb calls these endpoints from
// sb submit after all the individual PRs are created / retargeted.

// stackAPIVersion is the value we send in the X-GitHub-Api-Version header.
// Stacked PRs is a versioned feature; pin so gh's default doesn't drift.
const stackAPIVersion = "2026-03-10"

// StackInfo is the subset of a stack response we care about.
type StackInfo struct {
	Number       int
	PullRequests []int // ordered bottom-to-top
}

type stackWire struct {
	Number       int `json:"number"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

func (w stackWire) toInfo() *StackInfo {
	si := &StackInfo{Number: w.Number}
	for _, p := range w.PullRequests {
		si.PullRequests = append(si.PullRequests, p.Number)
	}
	return si
}

// RepoOwnerName returns the (owner, name) for the current git repo's default
// GitHub remote, via `gh repo view`.
func RepoOwnerName() (string, string, error) {
	out, err := run("repo", "view", "--json", "owner,name", "-q", `.owner.login+"/"+.name`)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(strings.TrimSpace(out), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("unexpected `gh repo view` output: %q", out)
	}
	return parts[0], parts[1], nil
}

// StackForPR returns the stack containing the given PR, or nil if none exists.
// Returns (nil, nil) if the repo doesn't have stacked PRs enabled (404) so
// callers can degrade gracefully.
func StackForPR(owner, repo string, prNumber int) (*StackInfo, error) {
	path := fmt.Sprintf("repos/%s/%s/stacks?pull_request=%d", owner, repo, prNumber)
	out, err := apiGet(path)
	if err != nil {
		if is404(err) {
			return nil, nil
		}
		return nil, err
	}
	if out == "" || out == "null" {
		return nil, nil
	}
	var wires []stackWire
	if err := json.Unmarshal([]byte(out), &wires); err != nil {
		return nil, err
	}
	if len(wires) == 0 {
		return nil, nil
	}
	return wires[0].toInfo(), nil
}

// CreateStack creates a stack via POST /repos/{owner}/{repo}/stacks.
// Requires at least two PR numbers, ordered bottom-to-top.
func CreateStack(owner, repo string, prNumbers []int) (*StackInfo, error) {
	path := fmt.Sprintf("repos/%s/%s/stacks", owner, repo)
	return postStack(path, prNumbers)
}

// AddToStack appends PRs to an existing stack (POST /stacks/{n}/add).
func AddToStack(owner, repo string, stackNumber int, prNumbers []int) (*StackInfo, error) {
	path := fmt.Sprintf("repos/%s/%s/stacks/%d/add", owner, repo, stackNumber)
	return postStack(path, prNumbers)
}

func postStack(path string, prNumbers []int) (*StackInfo, error) {
	body, err := json.Marshal(map[string]any{"pull_requests": prNumbers})
	if err != nil {
		return nil, err
	}
	out, err := apiPost(path, string(body))
	if err != nil {
		return nil, err
	}
	var wire stackWire
	if err := json.Unmarshal([]byte(out), &wire); err != nil {
		return nil, err
	}
	return wire.toInfo(), nil
}

// apiGet runs `gh api <path>` with the stacked-PRs API version pinned.
func apiGet(path string) (string, error) {
	return run("api", "-H", "X-GitHub-Api-Version: "+stackAPIVersion, path)
}

// apiPost runs `gh api <path> --method POST` with the given JSON body piped
// on stdin and the stacked-PRs API version pinned.
func apiPost(path, body string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(
		"gh", "api",
		"--method", "POST",
		"-H", "Content-Type: application/json",
		"-H", "X-GitHub-Api-Version: "+stackAPIVersion,
		"--input", "-",
		path,
	)
	cmd.Stdin = strings.NewReader(body)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh api POST %s: %s", path, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func is404(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "HTTP 404") || strings.Contains(s, "Not Found")
}

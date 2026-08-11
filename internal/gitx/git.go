// Package gitx wraps the `git` CLI. It never uses libgit2 and never talks to a
// remote directly — every remote op goes through `gh` or through git's own
// remote handling.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// runInteractive runs git with stdin/stdout/stderr wired to the terminal, so
// operations like `rebase` can prompt the user (editor, conflict resolution).
func runInteractive(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func RepoRoot() (string, error) {
	return run("git", "rev-parse", "--show-toplevel")
}

// GitDir returns the path to the .git directory (or worktree gitdir).
func GitDir() (string, error) {
	return run("git", "rev-parse", "--git-dir")
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

// AmendNoEdit reuses the current commit message and adds staged changes.
func AmendNoEdit() error {
	_, err := run("git", "commit", "--amend", "--no-edit")
	return err
}

// AmendMessage amends the current commit with a new message (leaving the tree
// unchanged unless there are staged changes).
func AmendMessage(msg string) error {
	_, err := run("git", "commit", "--amend", "-m", msg)
	return err
}

// HasStagedChanges reports whether the index differs from HEAD.
func HasStagedChanges() (bool, error) {
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
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

// UnsetConfig removes a config key. It's not an error if the key was unset.
func UnsetConfig(key string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("git", "config", "--unset", key)
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	// git config --unset exits 5 when the key doesn't exist.
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 5 {
		return nil
	}
	return fmt.Errorf("git config --unset %s: %s", key, strings.TrimSpace(stderr.String()))
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

// BranchExists reports whether a local branch exists.
func BranchExists(name string) (bool, error) {
	err := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+name).Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// RevParse returns the SHA of a rev.
func RevParse(rev string) (string, error) {
	return run("git", "rev-parse", "--verify", rev)
}

// HeadSha returns the SHA at HEAD of the given branch (or HEAD if empty).
func HeadSha(branch string) (string, error) {
	if branch == "" {
		return RevParse("HEAD")
	}
	return RevParse("refs/heads/" + branch)
}

// IsAncestor reports whether `ancestor` is an ancestor of `descendant`.
func IsAncestor(ancestor, descendant string) (bool, error) {
	err := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant).Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// RebaseOnto runs `git rebase --onto newBase oldBase branch`. Returns
// ErrRebaseConflict if git exits with a conflict.
func RebaseOnto(newBase, oldBase, branch string) error {
	err := runInteractive("rebase", "--onto", newBase, oldBase, branch)
	if err == nil {
		return nil
	}
	if RebaseInProgress() {
		return ErrRebaseConflict
	}
	return fmt.Errorf("git rebase --onto %s %s %s: %w", newBase, oldBase, branch, err)
}

// ErrRebaseConflict is returned by rebase helpers when git leaves a rebase in
// progress (conflict, or user asked to edit).
var ErrRebaseConflict = errors.New("rebase paused (likely a conflict) — resolve and run `sb continue`")

// RebaseInProgress reports whether a rebase is currently in progress.
func RebaseInProgress() bool {
	gitDir, err := GitDir()
	if err != nil {
		return false
	}
	// git rebase creates either rebase-merge/ or rebase-apply/.
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, d)); err == nil {
			return true
		}
	}
	return false
}

// RebaseContinue attempts to continue an in-progress rebase.
func RebaseContinue() error {
	err := runInteractive("rebase", "--continue")
	if err == nil {
		return nil
	}
	if RebaseInProgress() {
		return ErrRebaseConflict
	}
	return fmt.Errorf("git rebase --continue: %w", err)
}

// RebaseAbort aborts an in-progress rebase, if any.
func RebaseAbort() error {
	if !RebaseInProgress() {
		return nil
	}
	_, err := run("git", "rebase", "--abort")
	return err
}

// Fetch runs `git fetch [remote]` (default: origin).
func Fetch(remote string) error {
	if remote == "" {
		remote = "origin"
	}
	return runInteractive("fetch", remote)
}

// FetchPrune runs `git fetch --prune [remote]` so stale remote-tracking refs
// (branches deleted upstream) are cleaned up as part of the fetch.
func FetchPrune(remote string) error {
	if remote == "" {
		remote = "origin"
	}
	return runInteractive("fetch", "--prune", remote)
}

// BranchesWithGoneUpstream returns local branches whose configured upstream
// ref no longer exists on the remote. Requires a prior `git fetch --prune`.
//
// Uses `git for-each-ref` with `%(upstream:track)`, which prints `[gone]`
// when the tracked ref is missing.
func BranchesWithGoneUpstream() ([]string, error) {
	out, err := run("git", "for-each-ref",
		"--format=%(refname:short)\t%(upstream:track)",
		"refs/heads/",
	)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var gone []string
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		if strings.Contains(parts[1], "gone") {
			gone = append(gone, parts[0])
		}
	}
	return gone, nil
}

// RemoteExists reports whether the given remote is configured.
func RemoteExists(remote string) (bool, error) {
	out, err := run("git", "remote")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == remote {
			return true, nil
		}
	}
	return false, nil
}

// HasUpstream reports whether the branch has an upstream configured.
func HasUpstream(branch string) (bool, error) {
	_, err := run("git", "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err == nil {
		return true, nil
	}
	return false, nil
}

// PushForceWithLease pushes the branch to remote, force-with-lease, setting
// upstream if not already set.
func PushForceWithLease(remote, branch string) error {
	hasUp, _ := HasUpstream(branch)
	args := []string{"push", "--force-with-lease"}
	if !hasUp {
		args = append(args, "--set-upstream")
	}
	args = append(args, remote, branch)
	return runInteractive(args...)
}

// FastForward moves branch to target, without checking it out. If branch is
// checked out, we fall back to `git merge --ff-only`.
func FastForward(branch, target string) error {
	current, _ := CurrentBranch()
	if current == branch {
		_, err := run("git", "merge", "--ff-only", target)
		return err
	}
	_, err := run("git", "branch", "-f", branch, target)
	return err
}

// DeleteBranch deletes a local branch (with -D so merged-check is skipped).
func DeleteBranch(branch string) error {
	_, err := run("git", "branch", "-D", branch)
	return err
}

// InsideRepo reports whether cwd is inside a git repo.
func InsideRepo() bool {
	_, err := RepoRoot()
	return err == nil
}

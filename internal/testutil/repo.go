// Package testutil provides helpers for tests that need a real git repo.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Repo is a temporary git repo scoped to a test. On cleanup it removes the
// directory and restores the working directory.
type Repo struct {
	Dir     string
	origDir string
	t       *testing.T
}

// NewRepo creates a temp git repo with a single initial commit on `main` and
// chdir's into it. Author identity is set locally so tests don't depend on the
// developer's git config.
func NewRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	r := &Repo{Dir: dir, origDir: orig, t: t}
	t.Cleanup(r.Close)

	r.MustGit("init", "-q", "-b", "main")
	r.MustGit("config", "user.email", "test@example.com")
	r.MustGit("config", "user.name", "test")
	r.MustGit("config", "commit.gpgsign", "false")
	// Ensure rebase doesn't drop into an editor.
	os.Setenv("GIT_SEQUENCE_EDITOR", "true")
	os.Setenv("GIT_EDITOR", "true")

	r.WriteFile("README.md", "hello\n")
	r.MustGit("add", "README.md")
	r.MustGit("commit", "-qm", "initial")
	return r
}

func (r *Repo) Close() {
	if r.origDir != "" {
		_ = os.Chdir(r.origDir)
	}
}

// MustGit runs a git command; fatal on error.
func (r *Repo) MustGit(args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// Git runs a git command; returns stdout and error.
func (r *Repo) Git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	return string(out), err
}

// WriteFile writes a file inside the repo (relative to Dir).
func (r *Repo) WriteFile(rel, content string) {
	r.t.Helper()
	path := filepath.Join(r.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// StageAndCommit stages the given path and commits with the given message.
func (r *Repo) StageAndCommit(path, msg string) {
	r.MustGit("add", path)
	r.MustGit("commit", "-qm", msg)
}

// Head returns the SHA of a branch's HEAD (or HEAD if branch is empty).
func (r *Repo) Head(branch string) string {
	r.t.Helper()
	rev := "HEAD"
	if branch != "" {
		rev = "refs/heads/" + branch
	}
	out, err := exec.Command("git", "rev-parse", rev).Output()
	if err != nil {
		r.t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return string(out[:len(out)-1])
}

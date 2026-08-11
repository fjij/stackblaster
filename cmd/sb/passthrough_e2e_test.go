package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildOnce builds the sb binary into a temp dir and returns its path. The
// binary is shared across tests in this package via sync.Once semantics
// (implemented by ordering: TestMain builds first).
var sbBinary string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "sb-e2e-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	sbBinary = filepath.Join(tmp, "sb")
	build := exec.Command("go", "build", "-o", sbBinary, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("failed to build sb binary: " + err.Error())
	}
	os.Exit(m.Run())
}

// setupRepo creates a fresh temp git repo, chdirs into it, and returns the
// original working directory so the caller can defer restoring it.
func setupRepo(t *testing.T) (dir, orig string) {
	t.Helper()
	dir = t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	must := func(args ...string) {
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	must("init", "-q", "-b", "main")
	must("config", "user.email", "test@example.com")
	must("config", "user.name", "test")
	must("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must("add", "README.md")
	must("commit", "-qm", "initial")
	return dir, orig
}

func TestPassthrough_RevParseHead(t *testing.T) {
	setupRepo(t)

	out, err := exec.Command(sbBinary, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("sb rev-parse HEAD: %v", err)
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) != 40 {
		t.Fatalf("expected 40-char SHA, got %q", sha)
	}
	// Sanity check against git directly.
	direct, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(direct)) != sha {
		t.Fatalf("sb (%s) and git (%s) disagree", sha, strings.TrimSpace(string(direct)))
	}
}

func TestPassthrough_ExitCodePropagates(t *testing.T) {
	setupRepo(t)

	// `git config --get nonexistent.key` exits 1.
	cmd := exec.Command(sbBinary, "config", "--get", "some.nonexistent.key")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit")
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if ee.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got %d", ee.ExitCode())
	}
}

func TestPassthrough_KnownCommandStillCobra(t *testing.T) {
	// `sb log --help` should print sb's help, not git's log help.
	out, err := exec.Command(sbBinary, "log", "--help").Output()
	if err != nil {
		t.Fatalf("sb log --help: %v", err)
	}
	// sbParent is a git config key sb invents — git log would never mention it.
	if !strings.Contains(string(out), "sbParent") {
		t.Fatalf("expected sb log help (containing 'sbParent'), got:\n%s", out)
	}
}

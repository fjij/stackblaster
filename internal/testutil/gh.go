package testutil

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// GhStub is the handle a test uses to control and inspect the fake gh binary.
type GhStub struct {
	// PRsDir is where the stub reads/writes PR JSON fixtures. Tests can
	// pre-populate <branch>.json here to simulate an existing PR.
	PRsDir string
	// StacksDir is where the stub persists Stacked-PR API state (state.json).
	StacksDir string
	// LogPath is the file the stub appends each invocation's argv to (tab-
	// separated per line). Read via ReadLog / Calls.
	LogPath string

	t *testing.T
}

// SetupGhStub builds (once per test process) the ghstub binary and installs
// it as `gh` on PATH, scoped to the test. Also sets GH_STUB_LOG and
// GH_STUB_PRS_DIR to per-test temp paths so tests don't collide.
//
// Callers can further tweak the stub via SetMerged / SetUnauthed on the
// returned handle before invoking sb code.
func SetupGhStub(t *testing.T) *GhStub {
	t.Helper()
	binPath, err := buildGhStub()
	if err != nil {
		t.Fatalf("build gh stub: %v", err)
	}
	// Create a tempdir and copy (not symlink — PATH resolution follows
	// symlinks) the built binary in as `gh`.
	binDir := t.TempDir()
	dest := filepath.Join(binDir, "gh")
	if err := copyFile(binPath, dest, 0o755); err != nil {
		t.Fatalf("copy gh stub: %v", err)
	}

	prsDir := t.TempDir()
	stacksDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gh.log")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_STUB_PRS_DIR", prsDir)
	t.Setenv("GH_STUB_STACKS_DIR", stacksDir)
	t.Setenv("GH_STUB_LOG", logPath)
	t.Setenv("GH_STUB_UNAUTHED", "") // clear if a prior test set it

	return &GhStub{PRsDir: prsDir, StacksDir: stacksDir, LogPath: logPath, t: t}
}

// SetMerged tells the stub which branch names should appear in the
// `gh pr list --state merged` output.
func (g *GhStub) SetMerged(branches ...string) {
	g.t.Setenv("GH_STUB_MERGED", strings.Join(branches, ","))
}

// SetUnauthed makes `gh auth status` exit non-zero.
func (g *GhStub) SetUnauthed() {
	g.t.Setenv("GH_STUB_UNAUTHED", "1")
}

// ReadLog returns the full contents of the invocation log. Each line is a
// tab-separated argv from one call.
func (g *GhStub) ReadLog() string {
	b, err := os.ReadFile(g.LogPath)
	if err != nil {
		return ""
	}
	return string(b)
}

// Calls returns each logged invocation as a slice of its argv (tab-split).
func (g *GhStub) Calls() [][]string {
	log := g.ReadLog()
	if log == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	out := make([][]string, len(lines))
	for i, line := range lines {
		out[i] = strings.Split(line, "\t")
	}
	return out
}

// SeedPR writes a fixture PR file for the given branch so `gh pr view <branch>`
// returns it.
func (g *GhStub) SeedPR(branch, base string, number int) {
	g.t.Helper()
	pr := `{"number":` + itoa(number) + `,"state":"OPEN","isDraft":true,` +
		`"baseRefName":"` + base + `","headRefName":"` + branch + `",` +
		`"url":"https://github.com/fake/fake/pull/` + itoa(number) + `",` +
		`"title":"seeded"}`
	path := filepath.Join(g.PRsDir, branch+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		g.t.Fatalf("seed PR mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(pr), 0o644); err != nil {
		g.t.Fatalf("seed PR: %v", err)
	}
}

func itoa(n int) string {
	// Small helper; avoids strconv just for a couple call sites.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, mode)
}

// buildGhStub compiles the ghstub package once per test process. Callers get
// the path to the resulting binary.
var (
	ghStubPath string
	ghStubErr  error
	ghStubOnce sync.Once
)

func buildGhStub() (string, error) {
	ghStubOnce.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			ghStubErr = err
			return
		}
		dir, err := os.MkdirTemp("", "sb-ghstub-")
		if err != nil {
			ghStubErr = err
			return
		}
		out := filepath.Join(dir, "gh-stub")
		cmd := exec.Command("go", "build", "-o", out, "./internal/testutil/ghstub")
		cmd.Dir = root // tests chdir into temp repos, so pin build to the module root
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			ghStubErr = &buildErr{msg: err.Error(), stderr: stderr.String()}
			return
		}
		ghStubPath = out
	})
	return ghStubPath, ghStubErr
}

// moduleRoot walks up from this file's location until it finds a go.mod.
// Used to run `go build` from a stable directory even when a test has
// chdir'd into a temp repo.
func moduleRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed — cannot locate module root")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found walking up from " + thisFile)
		}
		dir = parent
	}
}

type buildErr struct {
	msg    string
	stderr string
}

func (e *buildErr) Error() string {
	if e.stderr != "" {
		return e.msg + ": " + e.stderr
	}
	return e.msg
}

// SetupBareOrigin creates a bare git repo, adds it as `origin` to `r`, and
// pushes r's `main` branch there. Returns the bare repo's path so tests can
// inspect it if needed.
func SetupBareOrigin(t *testing.T, r *Repo) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	r.MustGit("remote", "add", "origin", bare)
	r.MustGit("push", "-q", "origin", "main")
	return bare
}

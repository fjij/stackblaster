package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/testutil"
)

func TestSubmit_CreatesPRsBottomToTop(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	// main → A → B
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchB := mustCreate(t, "b")

	resetFlags()
	if err := runSubmit(nil, nil); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Expected calls in order: auth check, then per-branch (view, create), then top branch (view, create).
	calls := gh.Calls()
	if len(calls) == 0 {
		t.Fatal("expected gh to be invoked, log was empty")
	}
	// Verify each branch got a create with the right --head and --base.
	seen := map[string]string{} // head → base
	for _, argv := range calls {
		if len(argv) >= 2 && argv[0] == "pr" && argv[1] == "create" {
			head, base := parseCreateArgs(argv)
			seen[head] = base
		}
	}
	if seen[branchA] != "main" {
		t.Errorf("expected %s to be based on main, got %q (calls=%v)", branchA, seen[branchA], calls)
	}
	if seen[branchB] != branchA {
		t.Errorf("expected %s to be based on %s, got %q", branchB, branchA, seen[branchB])
	}
	// A's create must happen before B's create (bottom-to-top order).
	firstA, firstB := indexOfCreate(calls, branchA), indexOfCreate(calls, branchB)
	if firstA < 0 || firstB < 0 || firstA > firstB {
		t.Errorf("expected A create before B create; indices A=%d B=%d", firstA, firstB)
	}
}

func TestSubmit_UpdatesBaseWhenPRExistsAndBaseChanged(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	// main → A
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")

	// Seed a PR fixture claiming A is based on some-other-branch (stale).
	gh.SeedPR(branchA, "some-other-branch", 999)

	resetFlags()
	if err := runSubmit(nil, nil); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Expect a `pr edit <branchA> --base main` call.
	found := false
	for _, argv := range gh.Calls() {
		if len(argv) >= 3 && argv[0] == "pr" && argv[1] == "edit" && argv[2] == branchA {
			for i := 3; i < len(argv); i++ {
				if argv[i] == "--base" && i+1 < len(argv) && argv[i+1] == "main" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected `pr edit %s --base main`, got calls:\n%s", branchA, gh.ReadLog())
	}
}

func TestSubmit_TitleAndBodyFileOverrideCurrentBranchOnly(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	// main → A → B (B is current)
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "commit-A-subject")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchB := mustCreate(t, "commit-B-subject")

	bodyPath := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyPath, []byte("BODY-FROM-FILE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resetFlags()
	submitTitle = "OVERRIDDEN-TITLE"
	submitBodyFile = bodyPath
	if err := runSubmit(nil, nil); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Verify the PR fixture that got written for B has the overridden title.
	prB := readSeededPR(t, gh.PRsDir, branchB)
	if prB["title"] != "OVERRIDDEN-TITLE" {
		t.Errorf("expected B title override, got %q", prB["title"])
	}
	// The `pr create --head B` invocation should include the overridden body.
	found := false
	for _, argv := range gh.Calls() {
		if len(argv) >= 2 && argv[0] == "pr" && argv[1] == "create" {
			head, _ := parseCreateArgs(argv)
			if head != branchB {
				continue
			}
			for i := 0; i < len(argv); i++ {
				if argv[i] == "--body" && i+1 < len(argv) && strings.Contains(argv[i+1], "BODY-FROM-FILE") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected --body containing BODY-FROM-FILE in B create; log:\n%s", gh.ReadLog())
	}

	// Verify A's title came from its commit (override should NOT apply to A).
	prA := readSeededPR(t, gh.PRsDir, branchA)
	if prA["title"] != "commit-A-subject" {
		t.Errorf("expected A title from commit, got %q", prA["title"])
	}
}

func TestSync_PrunesMergedBranches(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	// main → A
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")

	// Return to main so we can delete A during prune.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}

	// Tell the stub A's PR is merged.
	gh.SetMerged(branchA)

	resetFlags()
	if err := runSync(nil, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	// A should have been deleted locally.
	exists, err := gitx.BranchExists(branchA)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("expected %s to be pruned; it still exists", branchA)
	}
}

// parseCreateArgs pulls --head and --base out of a `pr create ...` argv slice.
func parseCreateArgs(argv []string) (head, base string) {
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--head":
			if i+1 < len(argv) {
				head = argv[i+1]
			}
		case "--base":
			if i+1 < len(argv) {
				base = argv[i+1]
			}
		}
	}
	return
}

func indexOfCreate(calls [][]string, head string) int {
	for i, argv := range calls {
		if len(argv) >= 2 && argv[0] == "pr" && argv[1] == "create" {
			gotHead, _ := parseCreateArgs(argv)
			if gotHead == head {
				return i
			}
		}
	}
	return -1
}

func readSeededPR(t *testing.T, prsDir, branch string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(prsDir, branch+".json"))
	if err != nil {
		t.Fatalf("read PR fixture: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse PR fixture: %v", err)
	}
	return m
}

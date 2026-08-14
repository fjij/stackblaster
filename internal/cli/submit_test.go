package cli

import (
	"encoding/json"
	"os"
	"os/exec"
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

// TestSync_PrunesBranchDeletedFromOrigin covers the "close PR + delete
// branch" flow: the local branch should be pruned even though gh reports no
// merged PRs (nothing was merged — the PR was closed).
func TestSync_PrunesBranchDeletedFromOrigin(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t) // gh installed but reports no merged PRs
	_ = gh

	// Create and push branch A.
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")
	r.MustGit("push", "-q", "-u", "origin", branchA)

	// Simulate "close PR + delete branch on origin" — just delete the
	// remote branch. On the next fetch --prune, origin/A goes away and the
	// local branch's upstream becomes [gone].
	r.MustGit("push", "-q", "origin", "--delete", branchA)

	// Return to main so A can be deleted.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}

	resetFlags()
	if err := runSync(nil, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	exists, err := gitx.BranchExists(branchA)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("expected %s to be pruned; it still exists", branchA)
	}
}

// TestSubmit_LinksStackOnGitHub verifies that submit, after creating PRs for
// a 2+ branch stack, calls the /stacks REST endpoint to register them as a
// stack on GitHub.
func TestSubmit_LinksStackOnGitHub(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	// main → A → B
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	_ = mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	_ = mustCreate(t, "b")

	resetFlags()
	if err := runSubmit(nil, nil); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Expect a POST to /stacks with both PRs.
	found := false
	for _, argv := range gh.Calls() {
		if len(argv) < 2 || argv[0] != "api" {
			continue
		}
		joined := strings.Join(argv, " ")
		if strings.Contains(joined, "--method POST") && strings.Contains(joined, "repos/fake/fake/stacks") && !strings.Contains(joined, "?pull_request=") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a POST /repos/fake/fake/stacks call; got log:\n%s", gh.ReadLog())
	}
}

// TestSubmit_NoStackFlagSkipsLinking verifies that --no-stack suppresses the
// stack-linking API calls entirely.
func TestSubmit_NoStackFlagSkipsLinking(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	_ = mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	_ = mustCreate(t, "b")

	resetFlags()
	submitNoStack = true
	if err := runSubmit(nil, nil); err != nil {
		t.Fatalf("submit: %v", err)
	}

	for _, argv := range gh.Calls() {
		if len(argv) >= 2 && argv[0] == "api" && strings.Contains(strings.Join(argv, " "), "/stacks") {
			t.Fatalf("expected no /stacks calls under --no-stack, got: %v", argv)
		}
	}
}

// TestSubmit_DegradesGracefullyWithoutGh verifies that submit still pushes
// branches when gh is unavailable, just skipping PR creation/retargeting and
// stack linking. Simulated by making the gh stub report unauthed.
func TestSubmit_DegradesGracefullyWithoutGh(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	bare := testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)
	gh.SetUnauthed()

	// main → A
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")

	resetFlags()
	if err := runSubmit(nil, nil); err != nil {
		t.Fatalf("submit should not error when gh is unavailable: %v", err)
	}

	// No `pr` or `api` gh calls should have happened after the auth check
	// failed. `auth status` is expected.
	for _, argv := range gh.Calls() {
		if len(argv) < 1 {
			continue
		}
		switch argv[0] {
		case "pr", "api":
			t.Errorf("expected no %q calls when gh is unavailable, got: %v", argv[0], argv)
		}
	}

	// The branch must have been pushed to origin regardless.
	out, err := exec.Command("git", "--git-dir", bare, "rev-parse", "--verify", branchA).CombinedOutput()
	if err != nil {
		t.Fatalf("expected %s to exist on origin, got: %v\n%s", branchA, err, out)
	}
}

// TestSync_PrunesCurrentBranchByHoppingToTrunk covers the case where the
// branch we're actively on is the one that needs pruning. Sync should hop
// to trunk first and then delete the branch, rather than skipping it.
func TestSync_PrunesCurrentBranchByHoppingToTrunk(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	testutil.SetupGhStub(t)

	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")
	r.MustGit("push", "-q", "-u", "origin", branchA)
	r.MustGit("push", "-q", "origin", "--delete", branchA)

	// Deliberately do NOT hop back to main — we're on A when sync runs.

	resetFlags()
	if err := runSync(nil, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	exists, err := gitx.BranchExists(branchA)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("expected %s to be pruned; still exists", branchA)
	}
	cur, _ := gitx.CurrentBranch()
	if cur != "main" {
		t.Fatalf("expected to land on main after pruning current branch, got %s", cur)
	}
}

// TestSync_ReparentsChildrenWhenPruning covers a subtle case: sync prunes B,
// which is the parent of C. C's sbParent should be rewritten to B's sbParent
// (typically trunk) before B is deleted, or C ends up orphaned in `sb log`.
func TestSync_ReparentsChildrenWhenPruning(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	// main → A → B (A gets merged; B should survive with sbParent = main)
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchB := mustCreate(t, "b")

	// Sync deletes A (merged). B is our current branch — it survives with
	// its sbParent rewritten to main.
	gh.SetMerged(branchA)

	resetFlags()
	if err := runSync(nil, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if exists, _ := gitx.BranchExists(branchA); exists {
		t.Fatalf("expected %s to be pruned", branchA)
	}
	if exists, _ := gitx.BranchExists(branchB); !exists {
		t.Fatalf("expected %s to survive", branchB)
	}
	parent, err := gitx.GetConfig("branch." + branchB + ".sbParent")
	if err != nil {
		t.Fatal(err)
	}
	if parent != "main" {
		t.Fatalf("expected %s.sbParent to be reparented to main, got %q", branchB, parent)
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

// TestSync_SkipsRestackingMergedBranches is a regression test for a bug
// where `sb sync` attempted to rebase a merged branch onto the new trunk —
// but the branch's commits have already been squashed into trunk, so
// replaying them raises a conflict. Sync should detect the doomed branches
// before restacking and skip them.
//
// The scenario: user finishes reviewing a stack, all PRs get merged
// (squash-merges land on trunk), user runs `sb sync` while still checked
// out on the tip. Before this fix, sync would fail with a rebase conflict.
func TestSync_SkipsRestackingMergedBranches(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	testutil.SetupBareOrigin(t, r)
	gh := testutil.SetupGhStub(t)

	// main → A → B. Both will be "merged".
	r.WriteFile("shared.txt", "a-version\n")
	r.MustGit("add", "shared.txt")
	branchA := mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchB := mustCreate(t, "b")

	// Snapshot local main's SHA so we can rewind it after "pushing" the
	// squash-merge to origin. The scenario we need: origin/main is AHEAD of
	// local main, and the diff between them touches a file that A also
	// creates. If sync tried to rebase A onto the fast-forwarded main,
	// git would replay A's add-file commit onto a tree that already has
	// that file → conflict.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	originalMain, err := gitx.HeadSha("main")
	if err != nil {
		t.Fatal(err)
	}
	r.WriteFile("shared.txt", "squashed content\n")
	r.MustGit("add", "shared.txt")
	r.MustGit("commit", "-qm", "squash A and B")
	// Push the "merged" state to origin, then rewind local main so
	// fast-forward has something to actually pull in during sync.
	r.MustGit("push", "-q", "origin", "main")
	r.MustGit("reset", "--hard", "-q", originalMain)

	// Return to B — reproduces the bug's real-world trigger: user runs sync
	// from the tip of a fully-merged stack.
	if err := gitx.Checkout(branchB); err != nil {
		t.Fatal(err)
	}
	gh.SetMerged(branchA, branchB)

	resetFlags()
	if err := runSync(nil, nil); err != nil {
		t.Fatalf("sync unexpectedly errored (was previously a rebase conflict): %v", err)
	}

	// Both branches should be pruned; current branch should have hopped to main.
	for _, b := range []string{branchA, branchB} {
		if exists, _ := gitx.BranchExists(b); exists {
			t.Errorf("expected %s to be pruned; still exists", b)
		}
	}
	if cur, _ := gitx.CurrentBranch(); cur != "main" {
		t.Errorf("expected to be on main after prune; got %s", cur)
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

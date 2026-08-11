package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
	"github.com/fjij/stackblaster/internal/testutil"
)

// resetFlags puts command-scoped globals back to their zero values between
// sub-tests. This keeps tests independent when they invoke the run* functions
// directly.
func resetFlags() {
	createMsg = ""
	modifyMsg = ""
	modifyCommit = false
	moveOnto = ""
	trackParent = ""
	logAll = false
	checkoutAll = false
	syncNoPrune = false
	submitReady = false
	submitDraft = false
	submitDryRun = false
}

// silenceStdout redirects os.Stdout to /dev/null for the duration of a test —
// keeps go test -v output readable.
func silenceStdout(t *testing.T) {
	t.Helper()
	orig := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = devnull
	t.Cleanup(func() {
		devnull.Close()
		os.Stdout = orig
	})
}

func mustCreate(t *testing.T, msg string) string {
	t.Helper()
	resetFlags()
	createMsg = msg
	if err := runCreate(nil, nil); err != nil {
		t.Fatalf("create %q: %v", msg, err)
	}
	name, err := gitx.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestCreate_SetsParent(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Stage a file so create's initial commit is meaningful.
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")

	branch := mustCreate(t, "add file a")
	if !strings.Contains(branch, "add-file-a") {
		t.Fatalf("branch name missing slug: %s", branch)
	}
	parent, err := gitx.GetConfig("branch." + branch + ".sbParent")
	if err != nil {
		t.Fatal(err)
	}
	if parent != "main" {
		t.Fatalf("expected parent=main, got %s", parent)
	}
	// The commit exists.
	subj, _ := gitLogFormat(branch, "%s")
	if subj != "add file a" {
		t.Fatalf("expected commit subject 'add file a', got %q", subj)
	}
}

func TestModify_AmendsAndRestacksChild(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Build: main → A → B (linear)
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "branch a")

	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchB := mustCreate(t, "branch b")

	// Now amend A: check out A, modify it.
	if err := gitx.Checkout(branchA); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("a.txt", "a modified\n")
	r.MustGit("add", "a.txt")
	oldA := r.Head(branchA)
	resetFlags()
	if err := runModify(nil, nil); err != nil {
		t.Fatalf("modify: %v", err)
	}
	newA := r.Head(branchA)
	if oldA == newA {
		t.Fatal("modify didn't change A's SHA")
	}
	// B must have been rebased onto the new A.
	bParent := r.Head(branchB + "^")
	if bParent != newA {
		t.Fatalf("B not rebased onto new A: bParent=%s newA=%s", bParent, newA)
	}
	// Content check: B still contains b.txt.
	if err := gitx.Checkout(branchB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "b.txt")); err != nil {
		t.Fatalf("B lost its file: %v", err)
	}
}

func TestMove_ChangesParentAndRestacks(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Tree:
	//   main → A
	//   main → C (sibling of A)
	// Then move C onto A, so main → A → C.
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "branch a")

	// Back to main, make C.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("c.txt", "c\n")
	r.MustGit("add", "c.txt")
	branchC := mustCreate(t, "branch c")

	// Move C onto A.
	resetFlags()
	moveOnto = branchA
	if err := runMove(nil, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	// Assert sbParent.
	parent, err := gitx.GetConfig("branch." + branchC + ".sbParent")
	if err != nil {
		t.Fatal(err)
	}
	if parent != branchA {
		t.Fatalf("expected parent=%s, got %s", branchA, parent)
	}
	// Assert C^ == A tip.
	cParent := r.Head(branchC + "^")
	if cParent != r.Head(branchA) {
		t.Fatal("C not rebased onto A")
	}
	// Both files must be present in C's tree.
	for _, f := range []string{"a.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(r.Dir, f)); err != nil {
			t.Fatalf("%s missing after move: %v", f, err)
		}
	}
}

func TestMove_RefusesCycle(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchB := mustCreate(t, "b")

	// Try to move A onto its descendant B.
	if err := gitx.Checkout(branchA); err != nil {
		t.Fatal(err)
	}
	resetFlags()
	moveOnto = branchB
	err := runMove(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestSync_FastForwardsAndRestacks(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Build stack: main → A
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")
	aBeforeSync := r.Head(branchA)

	// Simulate a trunk advance by committing directly on main.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("trunk.txt", "trunk moved\n")
	r.MustGit("add", "trunk.txt")
	r.MustGit("commit", "-qm", "trunk advance")
	mainNew := r.Head("main")

	// Back to A.
	if err := gitx.Checkout(branchA); err != nil {
		t.Fatal(err)
	}
	// Sync (no origin — should skip fetch/prune and just restack).
	resetFlags()
	if err := runSync(nil, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	// A should now be based on mainNew.
	aParent := r.Head(branchA + "^")
	if aParent != mainNew {
		t.Fatalf("A not rebased onto new main: got %s want %s", aParent, mainNew)
	}
	if r.Head(branchA) == aBeforeSync {
		t.Fatal("A's SHA didn't change after sync")
	}
	// Trunk file is present.
	if _, err := os.Stat(filepath.Join(r.Dir, "trunk.txt")); err != nil {
		t.Fatalf("trunk.txt missing after sync: %v", err)
	}
}

func TestTrackUntrack(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Make a branch without sb create.
	r.MustGit("checkout", "-b", "manual-branch")
	resetFlags()
	trackParent = "main"
	if err := runTrack(nil, nil); err != nil {
		t.Fatalf("track: %v", err)
	}
	parent, err := gitx.GetConfig("branch.manual-branch.sbParent")
	if err != nil {
		t.Fatal(err)
	}
	if parent != "main" {
		t.Fatalf("expected parent=main, got %q", parent)
	}
	if err := runUntrack(nil, nil); err != nil {
		t.Fatalf("untrack: %v", err)
	}
	parent, _ = gitx.GetConfig("branch.manual-branch.sbParent")
	if parent != "" {
		t.Fatalf("expected untracked, still have parent=%q", parent)
	}
}

func TestNav_UpDownTopBottom(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// main → A → B → C
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		r.WriteFile(f, f+"\n")
		r.MustGit("add", f)
		mustCreate(t, f)
	}
	// Currently on C.
	resetFlags()
	if err := runDown(nil, nil); err != nil {
		t.Fatalf("down: %v", err)
	}
	// Should now be on B.
	cur, _ := gitx.CurrentBranch()
	if !strings.Contains(cur, "b-txt") {
		t.Fatalf("down: expected B, got %s", cur)
	}
	if err := runBottom(nil, nil); err != nil {
		t.Fatalf("bottom: %v", err)
	}
	cur, _ = gitx.CurrentBranch()
	if !strings.Contains(cur, "a-txt") {
		t.Fatalf("bottom: expected A, got %s", cur)
	}
	if err := runTop(nil, nil); err != nil {
		t.Fatalf("top: %v", err)
	}
	cur, _ = gitx.CurrentBranch()
	if !strings.Contains(cur, "c-txt") {
		t.Fatalf("top: expected C, got %s", cur)
	}
	if err := runDown(nil, nil); err != nil {
		t.Fatalf("down again: %v", err)
	}
	if err := runUp(nil, nil); err != nil {
		t.Fatalf("up: %v", err)
	}
	cur, _ = gitx.CurrentBranch()
	if !strings.Contains(cur, "c-txt") {
		t.Fatalf("up: expected C, got %s", cur)
	}
}

// buildFork sets up:
//
//	main → A → B
//	         → C
//
// and leaves the caller checked out on A. Returns the names of A, B, C.
func buildFork(t *testing.T, r *testutil.Repo) (a, b, c string) {
	t.Helper()
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	a = mustCreate(t, "a")

	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	b = mustCreate(t, "b")

	if err := gitx.Checkout(a); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("c.txt", "c\n")
	r.MustGit("add", "c.txt")
	c = mustCreate(t, "c")

	if err := gitx.Checkout(a); err != nil {
		t.Fatal(err)
	}
	return
}

func TestNav_UpNHops(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// main → A → B → C (linear)
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		r.WriteFile(f, f+"\n")
		r.MustGit("add", f)
		mustCreate(t, f)
	}
	// Currently on C (top). Go all the way to main.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}

	// `sb up 3` from main → should land on C.
	resetFlags()
	if err := runUp(nil, []string{"3"}); err != nil {
		t.Fatalf("up 3: %v", err)
	}
	cur, _ := gitx.CurrentBranch()
	if !strings.Contains(cur, "c-txt") {
		t.Fatalf("up 3: expected to land on c, got %s", cur)
	}
}

func TestNav_UpOvershoot(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// main → A (only 1 branch above main)
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	_ = mustCreate(t, "a")
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}

	// `sb up 5` from main → should stop at A (1 hop) and print a note.
	resetFlags()
	if err := runUp(nil, []string{"5"}); err != nil {
		t.Fatalf("up 5: %v", err)
	}
	cur, _ := gitx.CurrentBranch()
	if !strings.Contains(cur, "a") {
		t.Fatalf("up 5: expected to stop at A, got %s", cur)
	}
}

func TestNav_DownNHops(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// main → A → B → C
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		r.WriteFile(f, f+"\n")
		r.MustGit("add", f)
		mustCreate(t, f)
	}
	// Currently on C.

	// `sb down 2` → should land on A.
	resetFlags()
	if err := runDown(nil, []string{"2"}); err != nil {
		t.Fatalf("down 2: %v", err)
	}
	cur, _ := gitx.CurrentBranch()
	if !strings.Contains(cur, "a-txt") {
		t.Fatalf("down 2: expected to land on A, got %s", cur)
	}

	// `sb down 10` → should stop at main.
	resetFlags()
	if err := runDown(nil, []string{"10"}); err != nil {
		t.Fatalf("down 10: %v", err)
	}
	cur, _ = gitx.CurrentBranch()
	if cur != "main" {
		t.Fatalf("down 10: expected to stop at main, got %s", cur)
	}
}

func TestNav_ParseHopCount(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    int
		wantErr string
	}{
		{"no arg → 1", nil, 1, ""},
		{"3", []string{"3"}, 3, ""},
		{"0 → error", []string{"0"}, 0, "at least 1"},
		{"negative → error", []string{"-2"}, 0, "at least 1"},
		{"non-int → error", []string{"abc"}, 0, "positive integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHopCount(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error, got %d", got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error to contain %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

// TestNav_UpNUniqueSkipsPicker exercises the BFS shortcut: if exactly one
// branch lives at the requested distance, sb up N goes there without any
// picker interaction (even if there was a fork along the way).
func TestNav_UpNUniqueSkipsPicker(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Tree:
	//   main → A → B
	//   main → C
	// From main, sb up 2 should go to B (unique at distance 2). C at distance
	// 1 is not a valid distance-2 destination, so no picker.
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	branchA := mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchB := mustCreate(t, "b")

	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("c.txt", "c\n")
	r.MustGit("add", "c.txt")
	_ = mustCreate(t, "c")

	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	_ = branchA

	resetFlags()
	if err := runUp(nil, []string{"2"}); err != nil {
		t.Fatalf("up 2: %v", err)
	}
	cur, _ := gitx.CurrentBranch()
	if cur != branchB {
		t.Fatalf("expected to land on %s, got %s", branchB, cur)
	}
}

// TestNav_UpNMultipleErrorsInNonTTY: when N-distance has multiple candidates,
// the picker fires. In non-TTY (go test), that surfaces as an error mentioning
// `sb checkout`.
func TestNav_UpNMultipleErrorsInNonTTY(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Tree:
	//   main → A → B
	//   main → C → D
	// From main, sb up 2 → both B and D are at distance 2 → picker needed.
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	_ = mustCreate(t, "a")
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	_ = mustCreate(t, "b")

	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("c.txt", "c\n")
	r.MustGit("add", "c.txt")
	_ = mustCreate(t, "c")
	r.WriteFile("d.txt", "d\n")
	r.MustGit("add", "d.txt")
	_ = mustCreate(t, "d")

	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}

	resetFlags()
	err := runUp(nil, []string{"2"})
	if err == nil {
		t.Fatal("expected non-TTY picker error at up 2 with two branches at distance 2")
	}
	if !strings.Contains(err.Error(), "sb checkout") {
		t.Fatalf("expected error to point to sb checkout, got: %v", err)
	}
}

func TestNav_UpForkErrorsInNonTTY(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	_, _, _ = buildFork(t, r)

	// Two children under A; go test runs without a TTY, so PickBranch
	// returns ErrNotATTY and we surface a helpful error rather than hang.
	resetFlags()
	err := runUp(nil, nil)
	if err == nil {
		t.Fatal("expected error when up hits a fork without a TTY")
	}
	if !strings.Contains(err.Error(), "children") || !strings.Contains(err.Error(), "sb checkout") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNav_TopForkErrorsInNonTTY(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	_, _, _ = buildFork(t, r)

	// A has two leaves under it (B and C). Non-TTY, so top can't disambiguate.
	resetFlags()
	err := runTop(nil, nil)
	if err == nil {
		t.Fatal("expected error when top hits a fork without a TTY")
	}
	if !strings.Contains(err.Error(), "children") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTrack_NoParentErrorsInNonTTY(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	r.MustGit("checkout", "-b", "manual-branch")
	resetFlags() // trackParent = ""
	err := runTrack(nil, nil)
	if err == nil {
		t.Fatal("expected error when track has no --parent and no TTY")
	}
	if !strings.Contains(err.Error(), "--parent") {
		t.Fatalf("expected error to mention --parent, got: %v", err)
	}
}

func TestMove_NoOntoErrorsInNonTTY(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Create A off main so we have something to attempt a move on.
	r.WriteFile("a.txt", "a\n")
	r.MustGit("add", "a.txt")
	_ = mustCreate(t, "a")

	// Also make B off main so there's an eligible target — the not-TTY
	// error should fire before the "no eligible targets" check.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("b.txt", "b\n")
	r.MustGit("add", "b.txt")
	branchA := mustCreate(t, "b")
	_ = branchA

	resetFlags() // moveOnto = ""
	err := runMove(nil, nil)
	if err == nil {
		t.Fatal("expected error when move has no --onto and no TTY")
	}
	if !strings.Contains(err.Error(), "--onto") {
		t.Fatalf("expected error to mention --onto, got: %v", err)
	}
}

func TestCollectLeaves(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	_, branchB, branchC := buildFork(t, r)

	s, err := stack.Load("main")
	if err != nil {
		t.Fatal(err)
	}
	// A's leaves should be B and C (order-independent).
	branchA, _ := gitx.CurrentBranch()
	leaves := collectLeaves(s.All[branchA])
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leaves, got %d: %v", len(leaves), leaves)
	}
	got := map[string]bool{leaves[0]: true, leaves[1]: true}
	if !got[branchB] || !got[branchC] {
		t.Fatalf("expected leaves %s and %s, got %v", branchB, branchC, leaves)
	}
}

func TestModify_PersistsPlanOnConflict(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// main → A (writes file.txt=1) → B (writes file.txt=2 as amend-conflict target)
	r.WriteFile("file.txt", "1\n")
	r.MustGit("add", "file.txt")
	branchA := mustCreate(t, "a")

	r.WriteFile("file.txt", "2\n")
	r.MustGit("add", "file.txt")
	branchB := mustCreate(t, "b")

	// Now amend A to change the same file to a different value — this will
	// conflict when we try to restack B.
	if err := gitx.Checkout(branchA); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("file.txt", "999\n")
	r.MustGit("add", "file.txt")

	resetFlags()
	err := runModify(nil, nil)
	if err == nil {
		t.Fatal("expected conflict error from modify")
	}
	if !strings.Contains(err.Error(), "rebase paused") {
		t.Fatalf("expected rebase-paused error, got: %v", err)
	}
	// Plan should be persisted.
	plan, perr := stack.LoadPlan()
	if perr != nil {
		t.Fatal(perr)
	}
	if plan == nil {
		t.Fatal("expected a persisted plan")
	}
	if len(plan.Steps) == 0 {
		t.Fatal("expected at least one step in the plan")
	}
	if plan.Steps[0].Branch != branchB {
		t.Fatalf("expected first step to be B (%s), got %s", branchB, plan.Steps[0].Branch)
	}
	// Recover so cleanup works.
	_ = gitx.RebaseAbort()
	_ = stack.Delete()
}

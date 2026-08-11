package cli

import (
	"os"
	"path/filepath"
	"regexp"
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
	submitTitle = ""
	submitBodyFile = ""
	submitNoStack = false
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

// TestModify_CreatesFirstCommitOnEmptyBranch: after `sb create` without any
// staged changes, `sb modify` should create a new commit (not amend the
// parent's commit) once changes are staged. Uses the stored sbCreateMessage
// as the default commit message.
func TestModify_CreatesFirstCommitOnEmptyBranch(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Capture main's initial HEAD.
	mainInitial := r.Head("main")

	// sb create with no staged changes — branch is created but has no commit.
	resetFlags()
	createMsg = "add retry"
	if err := runCreate(nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	branch, _ := gitx.CurrentBranch()
	if r.Head(branch) != mainInitial {
		t.Fatalf("expected empty branch to point at main's HEAD, got %s vs %s",
			r.Head(branch), mainInitial)
	}
	// The intended message must have been stashed.
	stored, _ := gitx.GetConfig("branch." + branch + ".sbCreateMessage")
	if stored != "add retry" {
		t.Fatalf("expected stashed message 'add retry', got %q", stored)
	}

	// Now stage a change and run sb modify.
	r.WriteFile("retry.go", "// retry\n")
	r.MustGit("add", "retry.go")

	resetFlags()
	if err := runModify(nil, nil); err != nil {
		t.Fatalf("modify: %v", err)
	}

	// The branch must now have its own commit, and main must be unchanged.
	if r.Head("main") != mainInitial {
		t.Fatal("modify wrote to main's tip — should have created a new commit on the branch")
	}
	if r.Head(branch) == mainInitial {
		t.Fatal("branch HEAD didn't advance — no new commit was made")
	}
	// The new commit's parent must be main's tip.
	if r.Head(branch+"^") != mainInitial {
		t.Fatalf("new commit's parent should be main (%s), got %s",
			mainInitial, r.Head(branch+"^"))
	}
	// The stashed message must have been consumed (unset after use).
	stored, _ = gitx.GetConfig("branch." + branch + ".sbCreateMessage")
	if stored != "" {
		t.Fatalf("expected stashed message to be cleared, still have %q", stored)
	}
	// Commit message should be the stashed create message.
	subj, _ := gitLogFormat(branch, "%s")
	if subj != "add retry" {
		t.Fatalf("expected commit subject 'add retry', got %q", subj)
	}
}

// TestModify_EmptyBranchWithNothingStagedErrors: modify on an empty branch
// with nothing staged should error clearly rather than silently succeeding.
func TestModify_EmptyBranchWithNothingStagedErrors(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)
	_ = r

	resetFlags()
	createMsg = "add retry"
	if err := runCreate(nil, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	resetFlags()
	// Pass an -m so we get past the "no staged and no -m" pre-check and
	// exercise the "empty branch but nothing staged" branch.
	modifyMsg = "attempted"
	err := runModify(nil, nil)
	if err == nil {
		t.Fatal("expected error when modifying an empty branch with nothing staged")
	}
	if !strings.Contains(err.Error(), "no commits yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLog_ThreeSiblingsRenderSeparately: with three children of trunk, the
// tree renderer must emit two rejoin markers (one per non-primary sibling),
// otherwise adjacent siblings would look like a linear parent→child pair.
func TestLog_ThreeSiblingsRenderSeparately(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// Build three sibling branches all off main.
	for _, name := range []string{"a", "b", "c"} {
		if err := gitx.Checkout("main"); err != nil {
			t.Fatal(err)
		}
		r.WriteFile(name+".txt", name+"\n")
		r.MustGit("add", name+".txt")
		resetFlags()
		createMsg = name
		if err := runCreate(nil, nil); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	// Capture sb log output.
	if err := gitx.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		resetFlags()
		if err := runLog(nil, nil); err != nil {
			t.Fatalf("log: %v", err)
		}
	})
	// Strip ANSI so we can assert on structure.
	plain := stripANSI(out)

	// Expect 2 rejoin markers (one per non-primary sibling in a 3-child fork).
	rejoins := strings.Count(plain, "├──┘")
	if rejoins != 2 {
		t.Fatalf("expected 2 ├──┘ markers for 3 siblings, got %d\noutput:\n%s", rejoins, plain)
	}
	// All three branches must appear.
	for _, name := range []string{"a", "b", "c"} {
		suffix := "-" + name
		if !strings.Contains(plain, suffix) {
			t.Errorf("expected branch containing %q in output, got:\n%s", suffix, plain)
		}
	}
}

// captureStdout runs `fn` with os.Stdout redirected to a pipe and returns
// what was written. Used by rendering tests.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf strings.Builder
		b := make([]byte, 4096)
		for {
			n, err := r.Read(b)
			if n > 0 {
				buf.Write(b[:n])
			}
			if err != nil {
				break
			}
		}
		done <- buf.String()
	}()
	fn()
	w.Close()
	os.Stdout = orig
	return <-done
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestContinue_ResolvesConflictAndFinishesPlan(t *testing.T) {
	r := testutil.NewRepo(t)
	silenceStdout(t)

	// main → A (file.txt=1) → B (file.txt=2)
	r.WriteFile("file.txt", "1\n")
	r.MustGit("add", "file.txt")
	branchA := mustCreate(t, "a")
	r.WriteFile("file.txt", "2\n")
	r.MustGit("add", "file.txt")
	branchB := mustCreate(t, "b")

	// Amend A to conflict with B's edit.
	if err := gitx.Checkout(branchA); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("file.txt", "999\n")
	r.MustGit("add", "file.txt")

	// modify triggers the conflict during the restack of B.
	resetFlags()
	if err := runModify(nil, nil); err == nil {
		t.Fatal("expected conflict from modify")
	}
	if !gitx.RebaseInProgress() {
		t.Fatal("expected rebase in progress after conflict")
	}
	// Plan should be persisted with B as the pending step.
	plan, err := stack.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || len(plan.Steps) == 0 || plan.Steps[0].Branch != branchB {
		t.Fatalf("unexpected plan state: %+v", plan)
	}

	// Simulate the user resolving the conflict.
	r.WriteFile("file.txt", "resolved\n")
	r.MustGit("add", "file.txt")

	// sb continue should complete the rebase and drain the plan.
	resetFlags()
	if err := runContinue(nil, nil); err != nil {
		t.Fatalf("continue: %v", err)
	}
	if gitx.RebaseInProgress() {
		t.Fatal("rebase still in progress after continue")
	}
	if plan, _ := stack.LoadPlan(); plan != nil {
		t.Fatalf("expected plan to be cleared, got %+v", plan)
	}

	// The restack should have left B on the amended A tip, and B's file
	// should contain our resolution.
	bParent := r.Head(branchB + "^")
	if bParent != r.Head(branchA) {
		t.Errorf("B not based on new A tip after continue")
	}
	if err := gitx.Checkout(branchB); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(r.Dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "resolved" {
		t.Fatalf("expected 'resolved' after continue, got %q", got)
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

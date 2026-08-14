package cli

import (
	"testing"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/stack"
)

// buildTestStack constructs a stack from a parent map without touching git.
// Keys are branches; values are their sbParent (use "" for trunk itself).
// Trunk is inferred as the entry whose parent is "".
func buildTestStack(t *testing.T, trunk string, parents map[string]string) *stack.Stack {
	t.Helper()
	all := make(map[string]*stack.Node, len(parents))
	for name := range parents {
		all[name] = &stack.Node{Name: name}
	}
	for name, parent := range parents {
		all[name].Parent = parent
		if parent == "" {
			continue
		}
		p, ok := all[parent]
		if !ok {
			t.Fatalf("parent %q of %q missing from test stack", parent, name)
		}
		p.Children = append(p.Children, all[name])
	}
	root, ok := all[trunk]
	if !ok {
		t.Fatalf("trunk %q missing from test stack", trunk)
	}
	return &stack.Stack{Trunk: root, All: all}
}

func TestChainToTrunk_Linear(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "",
		"a":    "main",
		"b":    "a",
		"c":    "b",
	})
	chain, err := chainToTrunk(s, "c", "main")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c"}
	assertStrings(t, chain, want)
}

func TestChainToTrunk_CurrentIsBottom(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "",
		"a":    "main",
	})
	chain, err := chainToTrunk(s, "a", "main")
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, chain, []string{"a"})
}

func TestChainToTrunk_UntrackedReturnsNil(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "",
		"orphan": "", // no parent set — simulates untracked branch
	})
	chain, err := chainToTrunk(s, "orphan", "main")
	if err != nil {
		t.Fatal(err)
	}
	if chain != nil {
		t.Errorf("expected nil chain for untracked branch, got %v", chain)
	}
}

func TestChainToTrunk_MissingCurrent(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{"main": ""})
	chain, err := chainToTrunk(s, "ghost", "main")
	if err != nil {
		t.Fatal(err)
	}
	if chain != nil {
		t.Errorf("expected nil chain when current isn't in stack, got %v", chain)
	}
}

func TestBuildSubmitPlan_LinearWithFocus(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "",
		"a":    "main",
		"b":    "a",
		"c":    "b",
	})
	cfg := config.Config{DraftByDefault: true}
	plan, err := buildSubmitPlan(s, "c", "main", cfg, SubmitOpts{Focus: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(plan.Steps))
	}
	// Bottom-to-top: a, b, c. Parents match the tree.
	wantBranches := []string{"a", "b", "c"}
	wantParents := []string{"main", "a", "b"}
	for i, step := range plan.Steps {
		if step.Branch != wantBranches[i] {
			t.Errorf("step %d branch: want %s got %s", i, wantBranches[i], step.Branch)
		}
		if step.Parent != wantParents[i] {
			t.Errorf("step %d parent: want %s got %s", i, wantParents[i], step.Parent)
		}
	}
	// Only the focus branch should have IsFocus set.
	for _, step := range plan.Steps {
		want := step.Branch == "c"
		if step.IsFocus != want {
			t.Errorf("IsFocus for %s: want %v got %v", step.Branch, want, step.IsFocus)
		}
	}
	if !plan.LinkStack {
		t.Error("LinkStack should be true for a 3-branch stack")
	}
	if !plan.Draft {
		t.Error("Draft should follow DraftByDefault=true")
	}
}

func TestBuildSubmitPlan_SingleBranchSkipsLinking(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "",
		"solo": "main",
	})
	plan, err := buildSubmitPlan(s, "solo", "main", config.Config{}, SubmitOpts{Focus: "solo"})
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || len(plan.Steps) != 1 {
		t.Fatalf("want 1-step plan, got %+v", plan)
	}
	if plan.LinkStack {
		t.Error("LinkStack should be false for single-branch stack")
	}
}

func TestBuildSubmitPlan_NoStackOptDisablesLinking(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main", "b": "a",
	})
	plan, err := buildSubmitPlan(s, "b", "main", config.Config{}, SubmitOpts{Focus: "b", NoStack: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.LinkStack {
		t.Error("NoStack=true should suppress LinkStack")
	}
}

func TestBuildSubmitPlan_UntrackedReturnsNil(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main":   "",
		"orphan": "",
	})
	plan, err := buildSubmitPlan(s, "orphan", "main", config.Config{}, SubmitOpts{Focus: "orphan"})
	if err != nil {
		t.Fatal(err)
	}
	if plan != nil {
		t.Errorf("expected nil plan for untracked branch, got %+v", plan)
	}
}

func TestDecideDraft(t *testing.T) {
	cases := []struct {
		name          string
		draftDefault  bool
		ready, draft  bool
		want          bool
	}{
		{"default draft, no flags", true, false, false, true},
		{"default ready, no flags", false, false, false, false},
		{"--ready overrides draft default", true, true, false, false},
		{"--draft overrides ready default", false, false, true, true},
		{"--draft wins over --ready", true, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideDraft(config.Config{DraftByDefault: tc.draftDefault}, tc.ready, tc.draft)
			if got != tc.want {
				t.Errorf("want %v got %v", tc.want, got)
			}
		})
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("length: want %v got %v", want, got)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: want %s got %s", i, want[i], got[i])
		}
	}
}

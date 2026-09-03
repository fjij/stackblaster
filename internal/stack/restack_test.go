package stack

import (
	"testing"
)

// buildTestStack constructs a Stack from a parent map without touching git.
// Values of "" mean "this is the trunk".
func buildTestStack(t *testing.T, trunk string, parents map[string]string) *Stack {
	t.Helper()
	all := make(map[string]*Node, len(parents))
	for name := range parents {
		all[name] = &Node{Name: name}
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
	return &Stack{Trunk: root, All: all}
}

func fakeOldBaseFor(sha map[string]string) func(string) string {
	return func(branch string) string { return sha[branch] }
}

func findStep(steps []Step, branch string) *Step {
	for i := range steps {
		if steps[i].Branch == branch {
			return &steps[i]
		}
	}
	return nil
}

// Regression for the sync bug: when a branch is excluded (e.g. its PR merged),
// its surviving descendants must still be restacked — onto the closest
// non-excluded ancestor, not skipped along with the doomed branch.
func TestBuildPlanForChildren_ExcludedBranchStillRestacksItsChildren(t *testing.T) {
	// trunk → a (doomed) → b (active) → c (active)
	s := buildTestStack(t, "trunk", map[string]string{
		"trunk": "", "a": "trunk", "b": "a", "c": "b",
	})
	oldBase := fakeOldBaseFor(map[string]string{
		"a": "trunk_old", "b": "a_old", "c": "b_old",
	})
	steps, err := BuildPlanForChildren(s, "trunk", oldBase, map[string]bool{"a": true})
	if err != nil {
		t.Fatal(err)
	}

	if findStep(steps, "a") != nil {
		t.Errorf("a is excluded — should not appear in steps, got %+v", steps)
	}
	bStep := findStep(steps, "b")
	if bStep == nil {
		t.Fatalf("b should be restacked onto surviving ancestor, got %+v", steps)
	}
	if bStep.NewBase != "trunk" {
		t.Errorf("b.NewBase should be trunk (the survivor), got %q", bStep.NewBase)
	}
	if bStep.OldBase != "a_old" {
		t.Errorf("b.OldBase should be a's old tip, got %q", bStep.OldBase)
	}
	cStep := findStep(steps, "c")
	if cStep == nil {
		t.Fatalf("c should still be restacked as a descendant of b, got %+v", steps)
	}
	if cStep.NewBase != "b" {
		t.Errorf("c.NewBase should be b, got %q", cStep.NewBase)
	}
}

// Chain of excluded branches: the survivor walks all the way up.
func TestBuildPlanForChildren_ChainOfExcludedFallsThroughToSurvivor(t *testing.T) {
	// trunk → a (doomed) → b (doomed) → c (active)
	s := buildTestStack(t, "trunk", map[string]string{
		"trunk": "", "a": "trunk", "b": "a", "c": "b",
	})
	oldBase := fakeOldBaseFor(map[string]string{
		"a": "trunk_old", "b": "a_old", "c": "b_old",
	})
	steps, err := BuildPlanForChildren(s, "trunk", oldBase, map[string]bool{"a": true, "b": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Fatalf("only c should be restacked, got %+v", steps)
	}
	c := steps[0]
	if c.Branch != "c" || c.NewBase != "trunk" || c.OldBase != "b_old" {
		t.Errorf("c should rebase onto trunk from b_old, got %+v", c)
	}
}

// Excluded subtree with no surviving descendants: nothing to do.
func TestBuildPlanForChildren_ExcludedSubtreeAllDoomedProducesNoSteps(t *testing.T) {
	// trunk → a (doomed) → b (doomed)
	s := buildTestStack(t, "trunk", map[string]string{
		"trunk": "", "a": "trunk", "b": "a",
	})
	oldBase := fakeOldBaseFor(map[string]string{"a": "trunk_old", "b": "a_old"})
	steps, err := BuildPlanForChildren(s, "trunk", oldBase, map[string]bool{"a": true, "b": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Errorf("no survivors — expected empty plan, got %+v", steps)
	}
}

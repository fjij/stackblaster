package cli

import (
	"sort"
	"testing"
)

func TestBuildPrunePlan_Empty(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main",
	})
	plan := buildPrunePlan(s, "main", "a", nil)
	if plan == nil {
		t.Fatal("plan should never be nil")
	}
	if len(plan.Reparents) != 0 || len(plan.Delete) != 0 {
		t.Errorf("empty doomed set should produce empty plan, got %+v", plan)
	}
	if plan.HopToTrunk {
		t.Error("HopToTrunk should be false when current is safe")
	}
}

func TestBuildPrunePlan_ReparentsChildOfDoomed(t *testing.T) {
	// main → a → b → c ; b is doomed. c should reparent onto a.
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main", "b": "a", "c": "b",
	})
	plan := buildPrunePlan(s, "main", "c", map[string]string{"b": "PR merged"})

	if len(plan.Reparents) != 1 {
		t.Fatalf("want 1 reparent, got %v", plan.Reparents)
	}
	r := plan.Reparents[0]
	if r.Branch != "c" || r.OldParent != "b" || r.NewParent != "a" {
		t.Errorf("unexpected reparent: %+v", r)
	}
	if !containsString(plan.Delete, "b") {
		t.Errorf("Delete should contain b, got %v", plan.Delete)
	}
	if plan.HopToTrunk {
		t.Error("current is 'c', which survives — HopToTrunk should be false")
	}
}

func TestBuildPrunePlan_ChainOfDoomedReparentsToSurvivor(t *testing.T) {
	// main → a → b → c ; both a and b are doomed. c should reparent to main.
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main", "b": "a", "c": "b",
	})
	plan := buildPrunePlan(s, "main", "c", map[string]string{
		"a": "PR merged",
		"b": "PR merged",
	})

	// Only c reparents (b's child), since b's parent a is also doomed we
	// skip past it to main.
	var cReparent *PruneReparent
	for i := range plan.Reparents {
		if plan.Reparents[i].Branch == "c" {
			cReparent = &plan.Reparents[i]
		}
	}
	if cReparent == nil {
		t.Fatalf("c should be reparented, got %+v", plan.Reparents)
	}
	if cReparent.NewParent != "main" {
		t.Errorf("c should reparent to main (surviving ancestor), got %s", cReparent.NewParent)
	}
	// Both a and b are queued for deletion.
	sort.Strings(plan.Delete)
	assertStrings(t, plan.Delete, []string{"a", "b"})
}

func TestBuildPrunePlan_DoesNotReparentDoomedChildren(t *testing.T) {
	// main → a → b, both doomed. No reparent needed for b — it's going too.
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main", "b": "a",
	})
	plan := buildPrunePlan(s, "main", "main", map[string]string{
		"a": "gone from origin",
		"b": "PR merged",
	})
	for _, r := range plan.Reparents {
		if r.Branch == "b" {
			t.Errorf("b is doomed too — should not be reparented, got %+v", r)
		}
	}
}

func TestBuildPrunePlan_HopToTrunkWhenCurrentIsDoomed(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main",
	})
	plan := buildPrunePlan(s, "main", "a", map[string]string{"a": "PR merged"})
	if !plan.HopToTrunk {
		t.Error("current branch is doomed — HopToTrunk should be true")
	}
}

func TestBuildPrunePlan_DoomedBranchNotInStackStillDeleted(t *testing.T) {
	// Simulates a doomed branch that isn't in s.All (e.g. race with another
	// process). Plan should still include it in Delete but skip reparenting.
	s := buildTestStack(t, "main", map[string]string{"main": ""})
	plan := buildPrunePlan(s, "main", "main", map[string]string{"ghost": "gone from origin"})
	if !containsString(plan.Delete, "ghost") {
		t.Errorf("ghost branch should still be deleted, got %v", plan.Delete)
	}
	if len(plan.Reparents) != 0 {
		t.Errorf("no reparents expected for absent branch, got %+v", plan.Reparents)
	}
}

func TestResolveSurvivor_FirstNonDoomedAncestor(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main", "b": "a", "c": "b",
	})
	doomed := map[string]string{"b": "PR merged"}
	got := resolveSurvivor(s, "b", doomed, "main")
	if got != "a" {
		t.Errorf("want a, got %s", got)
	}
}

func TestResolveSurvivor_WalksPastMultipleDoomed(t *testing.T) {
	s := buildTestStack(t, "main", map[string]string{
		"main": "", "a": "main", "b": "a", "c": "b",
	})
	doomed := map[string]string{"a": "gone from origin", "b": "PR merged"}
	got := resolveSurvivor(s, "b", doomed, "main")
	if got != "main" {
		t.Errorf("want main, got %s", got)
	}
}

func TestResolveSurvivor_FallsBackToTrunk(t *testing.T) {
	// start with an empty ancestor chain — should still land on trunk.
	s := buildTestStack(t, "main", map[string]string{"main": ""})
	got := resolveSurvivor(s, "", map[string]string{}, "main")
	if got != "main" {
		t.Errorf("empty chain should fall back to trunk, got %s", got)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

package cli

import (
	"github.com/fjij/stackblaster/internal/stack"
)

// PruneReparent describes one child of a doomed branch being moved to a
// surviving ancestor. sb applies these before deleting the doomed branches
// so the tree doesn't leave orphans.
type PruneReparent struct {
	Branch    string
	OldParent string // the doomed branch being cut out
	NewParent string
}

// PrunePlan is what sb sync will do to trim doomed branches out of the tree:
// reparent their tracked children, then delete the doomed branches. The
// caller executes it against git; the plan itself is pure.
type PrunePlan struct {
	// Doomed maps branch → reason (for user-facing messages). This is the
	// input set, kept on the plan so callers can render it without a second
	// bag of state.
	Doomed     map[string]string
	Reparents  []PruneReparent
	Delete     []string // branches to `git branch -D`, order unspecified
	HopToTrunk bool     // true when the currently checked-out branch is doomed
}

// buildPrunePlan turns a set of doomed branches into the concrete reparent
// and delete steps. Pure — no git calls. Called by sb sync after external
// state (gone-upstream branches, merged PRs) has been gathered.
func buildPrunePlan(s *stack.Stack, trunk, current string, doomed map[string]string) *PrunePlan {
	plan := &PrunePlan{Doomed: doomed}
	if len(doomed) == 0 {
		return plan
	}
	for b := range doomed {
		node := s.All[b]
		if node == nil {
			// Doomed branch isn't in the stack (already gone?) — still
			// try to delete it.
			plan.Delete = append(plan.Delete, b)
			continue
		}
		newParent := resolveSurvivor(s, node.Parent, doomed, trunk)
		for _, child := range node.Children {
			if _, alsoDoomed := doomed[child.Name]; alsoDoomed {
				continue // child is going away too; nothing to reparent
			}
			plan.Reparents = append(plan.Reparents, PruneReparent{
				Branch:    child.Name,
				OldParent: b,
				NewParent: newParent,
			})
		}
		plan.Delete = append(plan.Delete, b)
	}
	_, plan.HopToTrunk = doomed[current]
	return plan
}

// resolveSurvivor walks up the sbParent chain from `start` and returns the
// first branch not in `doomed`. Falls back to `trunk` if we walk off the
// tree or the whole chain is doomed.
func resolveSurvivor(s *stack.Stack, start string, doomed map[string]string, trunk string) string {
	name := start
	seen := map[string]bool{}
	for name != "" && !seen[name] {
		if _, isDoomed := doomed[name]; !isDoomed {
			return name
		}
		seen[name] = true
		node, ok := s.All[name]
		if !ok {
			break
		}
		name = node.Parent
	}
	return trunk
}

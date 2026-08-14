package cli

import (
	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/stack"
)

// SubmitStep is one branch to submit. Steps are ordered bottom-to-top so
// parents exist by the time children reference them.
type SubmitStep struct {
	Branch  string
	Parent  string
	IsFocus bool // gets --title / --body-file overrides
}

// SubmitPlan is the ordered set of push+PR operations for sb submit.
type SubmitPlan struct {
	Steps     []SubmitStep
	Draft     bool // resolved once from cfg + flags
	LinkStack bool // true when we should link the PRs on GitHub after
}

// SubmitOpts bundles the flag-driven choices that flow into a submit plan.
// Keeping them off command-scoped globals makes the plan builder testable.
type SubmitOpts struct {
	Ready   bool
	Draft   bool
	NoStack bool
	Focus   string // branch whose PR receives --title / --body-file
}

// buildSubmitPlan walks the stack from `current` down to (but not including)
// `trunk` and returns the plan for submitting each branch. Returns
// (nil, nil) when the current branch is not tracked to trunk — callers
// should treat that as a user-facing error.
func buildSubmitPlan(s *stack.Stack, current, trunk string, cfg config.Config, opts SubmitOpts) (*SubmitPlan, error) {
	chain, err := chainToTrunk(s, current, trunk)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, nil
	}
	steps := make([]SubmitStep, len(chain))
	for i, b := range chain {
		steps[i] = SubmitStep{
			Branch:  b,
			Parent:  s.All[b].Parent,
			IsFocus: b == opts.Focus,
		}
	}
	return &SubmitPlan{
		Steps:     steps,
		Draft:     decideDraft(cfg, opts.Ready, opts.Draft),
		LinkStack: !opts.NoStack && len(steps) >= 2,
	}, nil
}

func decideDraft(cfg config.Config, ready, draft bool) bool {
	if draft {
		return true
	}
	if ready {
		return false
	}
	return cfg.DraftByDefault
}

// chainToTrunk returns branches from bottom (just above trunk) to top
// (current). Returns nil if the chain doesn't reach trunk.
func chainToTrunk(s *stack.Stack, current, trunk string) ([]string, error) {
	var reversed []string
	name := current
	seen := map[string]bool{}
	for name != "" && !seen[name] {
		seen[name] = true
		if name == trunk {
			out := make([]string, len(reversed))
			for i, b := range reversed {
				out[len(reversed)-1-i] = b
			}
			return out, nil
		}
		reversed = append(reversed, name)
		node, ok := s.All[name]
		if !ok || node.Parent == "" {
			return nil, nil
		}
		name = node.Parent
	}
	return nil, nil
}

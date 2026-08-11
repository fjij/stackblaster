package stack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fjij/stackblaster/internal/gitx"
)

// Step is a single pending rebase: rebase Branch --onto NewBase OldBase.
// OldBase is captured before we start touching anything, so descendants stay
// consistent even after their parent has already moved.
type Step struct {
	Branch  string `json:"branch"`
	NewBase string `json:"new_base"`
	OldBase string `json:"old_base"`
}

// Plan is a sequence of Steps plus a note for where to return after the last
// step. Persisted to .git/sb/plan.json so `sb continue` can resume it after a
// user resolves a conflict.
type Plan struct {
	Steps       []Step `json:"steps"`
	ReturnTo    string `json:"return_to,omitempty"` // branch to check out when done
	Description string `json:"description,omitempty"`
}

// BuildPlanForChildren returns the plan needed to restack every descendant of
// `root` onto the current tip of `root` (and cascading). newBaseFor tells the
// planner what SHA a given branch's children should rebase onto — this is
// usually the branch's current HEAD, captured *after* any modify/rebase has
// already reset it.
//
// oldBaseFor tells the planner where the branch used to be based, which is the
// old SHA of its parent branch at the time we started (i.e., before the
// user's amend or before sync fast-forwarded trunk).
func BuildPlanForChildren(s *Stack, root string, oldBaseFor func(branch string) string) ([]Step, error) {
	rootNode, ok := s.All[root]
	if !ok {
		return nil, fmt.Errorf("branch %q not tracked", root)
	}
	var steps []Step
	// BFS so parents are rebased before their children.
	queue := []*Node{rootNode}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, c := range n.Children {
			old := oldBaseFor(c.Name)
			steps = append(steps, Step{
				Branch:  c.Name,
				NewBase: n.Name, // resolved to a SHA at execution time
				OldBase: old,
			})
			queue = append(queue, c)
		}
	}
	return steps, nil
}

// Execute runs each step in order. If a rebase leaves a conflict, remaining
// steps (including a marker for the currently-conflicted one) are persisted
// via Save and gitx.ErrRebaseConflict is returned.
func (p *Plan) Execute() error {
	for len(p.Steps) > 0 {
		step := p.Steps[0]
		if err := runStep(step); err != nil {
			if errors.Is(err, gitx.ErrRebaseConflict) {
				// Save remaining steps (including this one so `sb continue`
				// knows what was in flight — after `git rebase --continue`
				// finishes it, we'll pop it).
				if saveErr := Save(p); saveErr != nil {
					return fmt.Errorf("conflict during rebase, and failed to save plan: %v (original: %w)", saveErr, err)
				}
				return err
			}
			return err
		}
		p.Steps = p.Steps[1:]
	}
	// All done — clean up any lingering plan file.
	_ = Delete()
	if p.ReturnTo != "" {
		return gitx.Checkout(p.ReturnTo)
	}
	return nil
}

func runStep(s Step) error {
	// Resolve NewBase to a SHA now. It might be a branch name given by the
	// planner; we snapshot its tip so intervening rebases don't shift it.
	newBaseSha, err := gitx.RevParse(s.NewBase)
	if err != nil {
		return fmt.Errorf("resolve new base %q: %w", s.NewBase, err)
	}
	oldBase := s.OldBase
	if oldBase == "" {
		// Fall back to the parent's current tip. Not ideal, but better than
		// aborting.
		oldBase = newBaseSha
	}
	return gitx.RebaseOnto(newBaseSha, oldBase, s.Branch)
}

// PlanPath returns the on-disk location for the persisted plan.
func PlanPath() (string, error) {
	gitDir, err := gitx.GitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "sb", "plan.json"), nil
}

func Save(p *Plan) error {
	path, err := PlanPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadPlan reads a persisted plan; returns (nil, nil) if none exists.
func LoadPlan() (*Plan, error) {
	path, err := PlanPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func Delete() error {
	path, err := PlanPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Descendants returns every branch reachable from root via children (excluding
// root itself), in BFS order.
func Descendants(s *Stack, root string) []string {
	n, ok := s.All[root]
	if !ok {
		return nil
	}
	var out []string
	queue := []*Node{n}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, c := range n.Children {
			out = append(out, c.Name)
			queue = append(queue, c)
		}
	}
	return out
}

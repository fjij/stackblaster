package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
	"github.com/fjij/stackblaster/internal/tui"
)

var moveOnto string

var moveCmd = &cobra.Command{
	Use:   "move [--onto TARGET]",
	Short: "Move the current branch to a new parent (rebase + retrack)",
	Long: `Change the current branch's parent to TARGET. Updates the sbParent
tracking, rebases the branch's commits onto TARGET, and restacks any
descendants.

Pass --onto to specify the target directly; omit it in a TTY to pick from
a list of eligible branches (descendants of the current branch are
filtered out, since moving onto one would create a cycle).`,
	RunE: runMove,
}

func init() {
	moveCmd.Flags().StringVar(&moveOnto, "onto", "", "new parent branch (opens a picker if omitted)")
	rootCmd.AddCommand(moveCmd)
}

func runMove(cmd *cobra.Command, args []string) error {
	if err := gitx.Preflight(); err != nil {
		return err
	}
	repoRoot, err := gitx.RepoRoot()
	if err != nil {
		return errors.New("must be run inside a git repository")
	}
	cfg, err := config.Load(repoRoot)
	if err != nil {
		return err
	}
	current, err := gitx.CurrentBranch()
	if err != nil {
		return err
	}
	if current == cfg.Trunk {
		return fmt.Errorf("refusing to move trunk (%s)", cfg.Trunk)
	}

	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}

	target := moveOnto
	if target == "" {
		target, err = pickTargetForMove(s, current, cfg.Trunk)
		if err != nil {
			return err
		}
		if target == "" {
			return nil // user canceled
		}
	}

	if target == current {
		return errors.New("cannot move a branch onto itself")
	}
	exists, err := gitx.BranchExists(target)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("target branch %q does not exist", target)
	}
	// Cycle check — enforced regardless of picker vs flag path.
	for _, d := range stack.Descendants(s, current) {
		if d == target {
			return fmt.Errorf("cannot move %s onto its descendant %s (cycle)", current, target)
		}
	}

	oldParent, err := gitx.GetConfig("branch." + current + ".sbParent")
	if err != nil {
		return err
	}
	if oldParent == "" {
		return fmt.Errorf("branch %q is not tracked (no sbParent) — run `sb track --parent %s` first", current, target)
	}
	if oldParent == target {
		fmt.Printf("✓ %s already has parent %s — nothing to do\n", current, target)
		return nil
	}

	// Snapshot pre-move SHAs so we can hand descendants the right oldBase.
	currentOldSha, err := gitx.HeadSha(current)
	if err != nil {
		return err
	}
	oldParentSha, err := gitx.HeadSha(oldParent)
	if err != nil {
		return err
	}
	newParentSha, err := gitx.HeadSha(target)
	if err != nil {
		return err
	}

	fmt.Printf("↻ moving %s: parent %s → %s\n", current, oldParent, target)
	if err := gitx.RebaseOnto(newParentSha, oldParentSha, current); err != nil {
		return err
	}

	if err := gitx.SetConfig("branch."+current+".sbParent", target); err != nil {
		return err
	}

	// Restack descendants. For direct children of `current`, the old base is
	// current's pre-move SHA. For grand-descendants, it's their own parent's
	// tip at plan-build time (which hasn't been rebased yet).
	oldBaseFor := func(branch string) string {
		node := s.All[branch]
		if node == nil || node.Parent == "" {
			return ""
		}
		if node.Parent == current {
			return currentOldSha
		}
		sha, err := gitx.HeadSha(node.Parent)
		if err != nil {
			return ""
		}
		return sha
	}
	steps, err := stack.BuildPlanForChildren(s, current, oldBaseFor)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		fmt.Printf("✓ moved %s\n", current)
		return nil
	}
	plan := &stack.Plan{
		Steps:       steps,
		ReturnTo:    current,
		Description: "restack descendants of " + current + " after move",
	}
	fmt.Printf("↻ restacking %d descendant(s)…\n", len(steps))
	return plan.Execute()
}

// pickTargetForMove shows a picker over every branch except `current` and
// its descendants (which would form a cycle). Highlights trunk and the
// current parent. Returns "" (no error) if the user canceled.
func pickTargetForMove(s *stack.Stack, current, trunk string) (string, error) {
	branches, err := gitx.ListBranches()
	if err != nil {
		return "", err
	}
	// Build the descendant set for the cycle filter.
	excluded := map[string]bool{current: true}
	for _, d := range stack.Descendants(s, current) {
		excluded[d] = true
	}
	currentParent, _ := gitx.GetConfig("branch." + current + ".sbParent")

	names := make([]string, 0, len(branches))
	items := make([]tui.PickerItem, 0, len(branches))
	for _, b := range branches {
		if excluded[b] {
			continue
		}
		names = append(names, b)
		item := tui.PickerItem{Name: b}
		switch {
		case b == currentParent:
			item.Hint = "(current parent)"
		case b == trunk:
			item.Hint = "(trunk)"
			item.Current = true // start the cursor on trunk
		}
		items = append(items, item)
	}
	if len(names) == 0 {
		return "", errors.New("no eligible branches to move onto — every other branch is a descendant of this one")
	}
	notTTYErr := fmt.Errorf(
		"no target given and this isn't a TTY — pass --onto BRANCH",
	)
	return pickFromBranches(names, items, "Move "+current+" — pick a new parent", notTTYErr)
}

package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var moveOnto string

var moveCmd = &cobra.Command{
	Use:   "move --onto TARGET",
	Short: "Move the current branch to a new parent (rebase + retrack)",
	Long: `Change the current branch's parent to TARGET. Updates the sbParent
tracking, rebases the branch's commits onto TARGET, and restacks any
descendants.

Refuses to move onto a descendant (that would create a cycle).`,
	RunE: runMove,
}

func init() {
	moveCmd.Flags().StringVar(&moveOnto, "onto", "", "new parent branch (required)")
	_ = moveCmd.MarkFlagRequired("onto")
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
	if current == moveOnto {
		return errors.New("cannot move a branch onto itself")
	}
	exists, err := gitx.BranchExists(moveOnto)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("target branch %q does not exist", moveOnto)
	}

	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}
	for _, d := range stack.Descendants(s, current) {
		if d == moveOnto {
			return fmt.Errorf("cannot move %s onto its descendant %s (cycle)", current, moveOnto)
		}
	}

	oldParent, err := gitx.GetConfig("branch." + current + ".sbParent")
	if err != nil {
		return err
	}
	if oldParent == "" {
		return fmt.Errorf("branch %q is not tracked (no sbParent) — run `sb track --parent %s` first", current, moveOnto)
	}
	if oldParent == moveOnto {
		fmt.Printf("✓ %s already has parent %s — nothing to do\n", current, moveOnto)
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
	newParentSha, err := gitx.HeadSha(moveOnto)
	if err != nil {
		return err
	}

	fmt.Printf("↻ moving %s: parent %s → %s\n", current, oldParent, moveOnto)
	if err := gitx.RebaseOnto(newParentSha, oldParentSha, current); err != nil {
		return err
	}

	if err := gitx.SetConfig("branch."+current+".sbParent", moveOnto); err != nil {
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

package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var (
	modifyMsg    string
	modifyCommit bool
)

var modifyCmd = &cobra.Command{
	Use:   "modify",
	Short: "Amend the current branch's commit and restack descendants",
	Long: `Stage your changes, then run sb modify to fold them into the current
branch's tip commit. Any descendant branches are automatically rebased onto
the new tip.

Local-only: sb modify never touches the remote. Run sb submit when you're
ready to push.

Pass -c/--commit to create a new commit instead of amending (useful if you
prefer the multi-commit workflow — sb doesn't force single-commit branches,
it just defaults to them).`,
	RunE: runModify,
}

func init() {
	modifyCmd.Flags().StringVarP(&modifyMsg, "message", "m", "", "new commit message (default: reuse existing)")
	modifyCmd.Flags().BoolVarP(&modifyCommit, "commit", "c", false, "create a new commit instead of amending")
	rootCmd.AddCommand(modifyCmd)
}

func runModify(cmd *cobra.Command, args []string) error {
	ctx, err := loadContext()
	if err != nil {
		return err
	}
	if err := ctx.requireCurrent(); err != nil {
		return err
	}
	current, cfg := ctx.Current, ctx.Cfg
	if current == cfg.Trunk {
		return fmt.Errorf("refusing to modify trunk (%s)", cfg.Trunk)
	}
	staged, err := gitx.HasStagedChanges()
	if err != nil {
		return err
	}
	if !staged && modifyMsg == "" {
		return errors.New("no staged changes and no -m — nothing to do")
	}

	oldSha, err := gitx.HeadSha(current)
	if err != nil {
		return err
	}

	// If this branch has no commits of its own (its HEAD is still equal to
	// the sbParent's HEAD), amending would rewrite the parent's commit —
	// which is wrong. Force a new commit instead. Matches gt's behavior.
	parentBranch, _ := gitx.GetConfig("branch." + current + ".sbParent")
	branchIsEmpty := false
	if parentBranch != "" {
		if parentSha, err := gitx.HeadSha(parentBranch); err == nil && parentSha == oldSha {
			branchIsEmpty = true
		}
	}

	// Commit or amend.
	if modifyCommit || branchIsEmpty {
		if !staged {
			if branchIsEmpty {
				return errors.New("branch has no commits yet — stage changes first (e.g. `git add -p`)")
			}
			return errors.New("--commit given but nothing is staged")
		}
		msg := modifyMsg
		if msg == "" && branchIsEmpty {
			// Fall back to the message the user passed to `sb create`.
			msg, _ = gitx.GetConfig("branch." + current + ".sbCreateMessage")
		}
		if msg == "" {
			msg = "wip"
		}
		if err := gitx.Commit(msg); err != nil {
			return err
		}
		if branchIsEmpty {
			// One-shot fallback — clear it so future modifies don't reuse a
			// stale message.
			_ = gitx.UnsetConfig("branch." + current + ".sbCreateMessage")
		}
	} else {
		if modifyMsg != "" {
			if err := gitx.AmendMessage(modifyMsg); err != nil {
				return err
			}
		} else {
			if err := gitx.AmendNoEdit(); err != nil {
				return err
			}
		}
	}

	newSha, err := gitx.HeadSha(current)
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s: %s → %s\n", current, short(oldSha), short(newSha))

	// Restack descendants.
	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}
	// Each descendant's old base is its parent's *pre-modify* SHA. For direct
	// children that's oldSha; for grand-descendants it's their parent's tip
	// as it was before we started — which, since we haven't touched them yet,
	// is just their current HEAD.
	oldBaseFor := func(branch string) string {
		node := s.All[branch]
		if node == nil || node.Parent == "" {
			return ""
		}
		if node.Parent == current {
			return oldSha
		}
		sha, err := gitx.HeadSha(node.Parent)
		if err != nil {
			return ""
		}
		return sha
	}
	steps, err := stack.BuildPlanForChildren(s, current, oldBaseFor, nil)
	if err != nil {
		return err
	}
	if len(steps) > 0 {
		plan := &stack.Plan{
			Steps:       steps,
			ReturnTo:    current,
			Description: "restack descendants of " + current + " after modify",
		}
		fmt.Printf("↻ restacking %d descendant(s)…\n", len(steps))
		if err := plan.Execute(); err != nil {
			return err
		}
	}
	return nil
}

func short(sha string) string {
	if len(sha) < 8 {
		return sha
	}
	return sha[:8]
}

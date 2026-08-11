package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
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
		return fmt.Errorf("resolve current branch: %w", err)
	}
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

	// Commit or amend.
	if modifyCommit {
		if !staged {
			return errors.New("--commit given but nothing is staged")
		}
		msg := modifyMsg
		if msg == "" {
			msg = "wip"
		}
		if err := gitx.Commit(msg); err != nil {
			return err
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
	steps, err := stack.BuildPlanForChildren(s, current, oldBaseFor)
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

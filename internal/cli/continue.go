package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var continueCmd = &cobra.Command{
	Use:   "continue",
	Short: "Resume the paused stack operation (typically after a conflict)",
	RunE:  runContinue,
}

func init() {
	rootCmd.AddCommand(continueCmd)
}

func runContinue(cmd *cobra.Command, args []string) error {
	if err := gitx.Preflight(); err != nil {
		return err
	}
	if _, err := gitx.RepoRoot(); err != nil {
		return errors.New("must be run inside a git repository")
	}

	if gitx.RebaseInProgress() {
		fmt.Println("↻ continuing git rebase…")
		if err := gitx.RebaseContinue(); err != nil {
			return err
		}
	}

	plan, err := stack.LoadPlan()
	if err != nil {
		return err
	}
	if plan == nil {
		fmt.Println("✓ no pending stack operation — nothing to continue")
		return nil
	}
	// The step that just finished (via rebase --continue) is the head of the
	// plan; pop it and keep going.
	if len(plan.Steps) > 0 {
		done := plan.Steps[0]
		plan.Steps = plan.Steps[1:]
		fmt.Printf("✓ %s rebased\n", done.Branch)
	}
	if len(plan.Steps) == 0 {
		_ = stack.Delete()
		if plan.ReturnTo != "" {
			return gitx.Checkout(plan.ReturnTo)
		}
		return nil
	}
	fmt.Printf("↻ %d step(s) remaining…\n", len(plan.Steps))
	return plan.Execute()
}

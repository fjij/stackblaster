package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var restackCmd = &cobra.Command{
	Use:   "restack",
	Short: "Rebase descendants of the current branch onto its current tip",
	RunE:  runRestack,
}

func init() {
	rootCmd.AddCommand(restackCmd)
}

func runRestack(cmd *cobra.Command, args []string) error {
	ctx, s, err := loadStackContext()
	if err != nil {
		return err
	}
	if err := ctx.requireCurrent(); err != nil {
		return err
	}
	current := ctx.Current
	// oldBase = parent's current tip. That's correct only when children still
	// point at commits reachable from that tip; if the user has manually moved
	// things around, they should pass --onto explicitly (future flag).
	oldBaseFor := func(branch string) string {
		node := s.All[branch]
		if node == nil || node.Parent == "" {
			return ""
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
	if len(steps) == 0 {
		fmt.Println("✓ nothing to restack — no descendants")
		return nil
	}
	plan := &stack.Plan{
		Steps:       steps,
		ReturnTo:    current,
		Description: "restack descendants of " + current,
	}
	fmt.Printf("↻ restacking %d descendant(s)…\n", len(steps))
	return plan.Execute()
}

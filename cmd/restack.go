package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
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
	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}
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
	steps, err := stack.BuildPlanForChildren(s, current, oldBaseFor)
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

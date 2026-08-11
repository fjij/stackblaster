package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
)

var trackParent string

var trackCmd = &cobra.Command{
	Use:   "track --parent BRANCH",
	Short: "Adopt the current branch into the stack under BRANCH",
	RunE:  runTrack,
}

var untrackCmd = &cobra.Command{
	Use:   "untrack",
	Short: "Forget the current branch's parent (leaves the git branch intact)",
	RunE:  runUntrack,
}

func init() {
	trackCmd.Flags().StringVar(&trackParent, "parent", "", "parent branch (defaults to trunk)")
	rootCmd.AddCommand(trackCmd, untrackCmd)
}

func runTrack(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("refusing to track trunk (%s)", cfg.Trunk)
	}
	parent := trackParent
	if parent == "" {
		parent = cfg.Trunk
	}
	if parent == current {
		return errors.New("parent cannot be the current branch")
	}
	exists, err := gitx.BranchExists(parent)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("parent branch %q does not exist", parent)
	}
	if err := gitx.SetConfig("branch."+current+".sbParent", parent); err != nil {
		return err
	}
	fmt.Printf("✓ tracking %s → %s\n", current, parent)
	return nil
}

func runUntrack(cmd *cobra.Command, args []string) error {
	if err := gitx.Preflight(); err != nil {
		return err
	}
	if _, err := gitx.RepoRoot(); err != nil {
		return errors.New("must be run inside a git repository")
	}
	current, err := gitx.CurrentBranch()
	if err != nil {
		return err
	}
	if err := gitx.UnsetConfig("branch." + current + ".sbParent"); err != nil {
		return err
	}
	fmt.Printf("✓ untracked %s\n", current)
	return nil
}

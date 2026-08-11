package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Move up one branch in the stack (toward the top)",
	RunE:  runUp,
}

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Move down one branch in the stack (toward trunk)",
	RunE:  runDown,
}

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Jump to the top of the current stack",
	RunE:  runTop,
}

var bottomCmd = &cobra.Command{
	Use:   "bottom",
	Short: "Jump to the branch just above trunk in the current stack",
	RunE:  runBottom,
}

func init() {
	rootCmd.AddCommand(upCmd, downCmd, topCmd, bottomCmd)
}

func loadStackAndCurrent() (*stack.Stack, string, config.Config, error) {
	if err := gitx.Preflight(); err != nil {
		return nil, "", config.Config{}, err
	}
	repoRoot, err := gitx.RepoRoot()
	if err != nil {
		return nil, "", config.Config{}, errors.New("must be run inside a git repository")
	}
	cfg, err := config.Load(repoRoot)
	if err != nil {
		return nil, "", cfg, err
	}
	current, err := gitx.CurrentBranch()
	if err != nil {
		return nil, "", cfg, err
	}
	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return nil, "", cfg, err
	}
	return s, current, cfg, nil
}

func runUp(cmd *cobra.Command, args []string) error {
	s, current, _, err := loadStackAndCurrent()
	if err != nil {
		return err
	}
	node, ok := s.All[current]
	if !ok || len(node.Children) == 0 {
		return errors.New("no branch above current in the stack")
	}
	if len(node.Children) > 1 {
		return fmt.Errorf(
			"branch %q has %d children — use `sb checkout` to pick one",
			current, len(node.Children),
		)
	}
	return gitx.Checkout(node.Children[0].Name)
}

func runDown(cmd *cobra.Command, args []string) error {
	s, current, cfg, err := loadStackAndCurrent()
	if err != nil {
		return err
	}
	node, ok := s.All[current]
	if !ok || node.Parent == "" {
		if current == cfg.Trunk {
			return errors.New("already at trunk")
		}
		return errors.New("current branch is not tracked (no parent to move down to)")
	}
	return gitx.Checkout(node.Parent)
}

func runTop(cmd *cobra.Command, args []string) error {
	s, current, _, err := loadStackAndCurrent()
	if err != nil {
		return err
	}
	node, ok := s.All[current]
	if !ok {
		return fmt.Errorf("current branch %q not tracked", current)
	}
	for len(node.Children) == 1 {
		node = node.Children[0]
	}
	if len(node.Children) > 1 {
		return fmt.Errorf(
			"branch %q has %d children — use `sb checkout` to pick one",
			node.Name, len(node.Children),
		)
	}
	if node.Name == current {
		fmt.Println("✓ already at top")
		return nil
	}
	return gitx.Checkout(node.Name)
}

func runBottom(cmd *cobra.Command, args []string) error {
	s, current, cfg, err := loadStackAndCurrent()
	if err != nil {
		return err
	}
	node, ok := s.All[current]
	if !ok {
		return fmt.Errorf("current branch %q not tracked", current)
	}
	for node.Parent != "" && node.Parent != cfg.Trunk {
		p, ok := s.All[node.Parent]
		if !ok {
			break
		}
		node = p
	}
	if node.Name == current {
		fmt.Println("✓ already at bottom")
		return nil
	}
	return gitx.Checkout(node.Name)
}

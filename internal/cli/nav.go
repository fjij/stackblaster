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
	if len(node.Children) == 1 {
		return gitx.Checkout(node.Children[0].Name)
	}
	names := make([]string, len(node.Children))
	for i, c := range node.Children {
		names[i] = c.Name
	}
	choice, err := pickOrErr(names, fmt.Sprintf("Up from %s — pick a child", current), len(node.Children), current)
	if err != nil {
		return err
	}
	if choice == "" {
		return nil // canceled
	}
	return gitx.Checkout(choice)
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
	// Walk up while the path is unambiguous.
	for len(node.Children) == 1 {
		node = node.Children[0]
	}
	if len(node.Children) == 0 {
		if node.Name == current {
			fmt.Println("✓ already at top")
			return nil
		}
		return gitx.Checkout(node.Name)
	}
	// Fork — collect every leaf reachable from here and offer them as a picker.
	leaves := collectLeaves(node)
	if len(leaves) == 1 {
		return gitx.Checkout(leaves[0])
	}
	choice, err := pickOrErr(leaves, fmt.Sprintf("Top from %s — pick a leaf", current), len(leaves), node.Name)
	if err != nil {
		return err
	}
	if choice == "" {
		return nil
	}
	return gitx.Checkout(choice)
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

// pickOrErr shows a picker over `names` for nav commands. See pickFromBranches
// for the general contract; this thin wrapper just supplies a nav-specific
// non-TTY error message.
func pickOrErr(names []string, title string, count int, atBranch string) (string, error) {
	notTTYErr := fmt.Errorf(
		"branch %q has %d children and this isn't a TTY — pick one with `sb checkout <branch>`",
		atBranch, count,
	)
	return pickFromBranches(names, nil, title, notTTYErr)
}

// collectLeaves returns the names of every leaf branch in the subtree rooted
// at n. If n itself is a leaf, returns [n.Name].
func collectLeaves(n *stack.Node) []string {
	if len(n.Children) == 0 {
		return []string{n.Name}
	}
	var out []string
	for _, c := range n.Children {
		out = append(out, collectLeaves(c)...)
	}
	return out
}

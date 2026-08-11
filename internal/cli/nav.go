package cli

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var upCmd = &cobra.Command{
	Use:   "up [N]",
	Short: "Move up N branches in the stack (default 1)",
	Long: `Moves toward the top of the stack. Without N, moves one branch;
with N, moves up to N branches. At each fork, the branch picker opens
so you can choose which child to follow.

If N exceeds the distance to a leaf, sb up stops at the leaf and prints
how far it got.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUp,
}

var downCmd = &cobra.Command{
	Use:   "down [N]",
	Short: "Move down N branches in the stack (default 1)",
	Long: `Moves toward trunk. Without N, moves one branch; with N, moves up
to N branches. Stops at trunk if N would take you further.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDown,
}

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Jump to the top of the current stack",
	Args:  cobra.NoArgs,
	RunE:  runTop,
}

var bottomCmd = &cobra.Command{
	Use:   "bottom",
	Short: "Jump to the branch just above trunk in the current stack",
	Args:  cobra.NoArgs,
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

// parseHopCount parses an optional positional "N" argument. N must be a
// positive integer.
func parseHopCount(args []string) (int, error) {
	if len(args) == 0 {
		return 1, nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("N must be a positive integer, got %q", args[0])
	}
	if n < 1 {
		return 0, fmt.Errorf("N must be at least 1 (got %d)", n)
	}
	return n, nil
}

func runUp(cmd *cobra.Command, args []string) error {
	n, err := parseHopCount(args)
	if err != nil {
		return err
	}
	s, current, _, err := loadStackAndCurrent()
	if err != nil {
		return err
	}

	candidates, depth := candidatesAtDepth(s, current, n)
	if depth == 0 {
		return errors.New("no branch above current in the stack")
	}

	var dest string
	if len(candidates) == 1 {
		dest = candidates[0]
	} else {
		title := fmt.Sprintf("Up %d from %s — pick a destination", depth, current)
		choice, err := pickOrErr(candidates, title, len(candidates), current)
		if err != nil {
			return err
		}
		if choice == "" {
			return nil // user canceled
		}
		dest = choice
	}

	if err := gitx.Checkout(dest); err != nil {
		return err
	}
	if depth < n {
		fmt.Printf("(only moved %d of %d — no branches at distance %d)\n", depth, n, n)
	}
	return nil
}

// candidatesAtDepth returns the set of branches reachable in exactly n hops up
// from `from`. If a path runs out of children before reaching depth n, the
// deepest reachable frontier is returned with `depth < n` so callers can offer
// a partial move.
//
// The key property this enables: if only one branch is reachable at the
// requested distance, we auto-navigate there without asking the user to pick
// through intermediate forks that are ambiguous now but converge to one
// answer at depth n.
func candidatesAtDepth(s *stack.Stack, from string, n int) (frontier []string, depth int) {
	frontier = []string{from}
	for i := 0; i < n; i++ {
		var next []string
		for _, name := range frontier {
			node, ok := s.All[name]
			if !ok {
				continue
			}
			for _, c := range node.Children {
				next = append(next, c.Name)
			}
		}
		if len(next) == 0 {
			if i == 0 {
				return nil, 0
			}
			sort.Strings(frontier)
			return frontier, i
		}
		frontier = next
	}
	sort.Strings(frontier)
	return frontier, n
}

func runDown(cmd *cobra.Command, args []string) error {
	n, err := parseHopCount(args)
	if err != nil {
		return err
	}
	s, current, cfg, err := loadStackAndCurrent()
	if err != nil {
		return err
	}
	dest, moved := planDown(s, current, n, cfg.Trunk)
	if moved == 0 {
		if current == cfg.Trunk {
			return errors.New("already at trunk")
		}
		return errors.New("current branch is not tracked (no parent to move down to)")
	}
	if err := gitx.Checkout(dest); err != nil {
		return err
	}
	if moved < n {
		fmt.Printf("(only moved %d of %d — %s is trunk)\n", moved, n, dest)
	}
	return nil
}

// planDown walks up to n hops toward trunk. Stops at trunk (inclusive) or at
// an untracked branch.
func planDown(s *stack.Stack, from string, n int, trunk string) (dest string, moved int) {
	dest = from
	for i := 0; i < n; i++ {
		if dest == trunk {
			return dest, moved
		}
		node, ok := s.All[dest]
		if !ok || node.Parent == "" {
			return dest, moved
		}
		dest = node.Parent
		moved++
	}
	return dest, moved
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

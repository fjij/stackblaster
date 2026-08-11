package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
	"github.com/fjij/stackblaster/internal/tui"
)

var checkoutAll bool

var checkoutCmd = &cobra.Command{
	Use:     "checkout [BRANCH]",
	Aliases: []string{"co"},
	Short:   "Interactive branch picker (or non-interactive if BRANCH given)",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runCheckout,
}

func init() {
	checkoutCmd.Flags().BoolVar(&checkoutAll, "all", false, "show every tracked branch, not just the current stack")
	rootCmd.AddCommand(checkoutCmd)
}

func runCheckout(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		return gitx.Checkout(args[0])
	}
	s, current, cfg, err := loadStackAndCurrent()
	if err != nil {
		return err
	}

	var names []string
	if checkoutAll {
		for _, n := range collectAll(s) {
			names = append(names, n)
		}
	} else {
		names = collectCurrentStack(s, current, cfg.Trunk)
	}
	if len(names) == 0 {
		return errors.New("no branches to check out")
	}
	items := make([]tui.PickerItem, len(names))
	for i, n := range names {
		items[i] = tui.PickerItem{Name: n, Current: n == current}
		if n == current {
			items[i].Hint = "(current)"
		} else if n == cfg.Trunk {
			items[i].Hint = "(trunk)"
		}
	}
	title := "Checkout — current stack"
	if checkoutAll {
		title = "Checkout — all tracked branches"
	}
	choice, err := tui.PickBranch(items, title)
	if err != nil {
		if errors.Is(err, tui.ErrNotATTY) {
			return errors.New("not a TTY — pass BRANCH as an argument")
		}
		if errors.Is(err, tui.ErrCanceled) {
			fmt.Fprintln(cmd.ErrOrStderr(), "canceled")
			return nil
		}
		return err
	}
	if choice == current {
		fmt.Printf("✓ already on %s\n", choice)
		return nil
	}
	return gitx.Checkout(choice)
}

// collectCurrentStack returns branches from top of stack (containing current)
// down to trunk.
func collectCurrentStack(s *stack.Stack, current, trunk string) []string {
	// Walk down from current to trunk to get the chain, then walk up from
	// current to include the top of the stack too.
	up := walkDown(s, current)
	down := walkUp(s, current)
	// Combine: top → current → trunk.
	out := make([]string, 0, len(down)+len(up))
	// Skip current in `up` because it's the first element of `down`.
	for i := len(up) - 1; i >= 0; i-- {
		if up[i] == current {
			continue
		}
		out = append(out, up[i])
	}
	out = append(out, down...)
	return out
}

// walkUp returns [current, single-child, ...] until a branching point or leaf.
func walkUp(s *stack.Stack, from string) []string {
	var out []string
	node, ok := s.All[from]
	if !ok {
		return out
	}
	for node != nil {
		out = append(out, node.Name)
		if len(node.Children) != 1 {
			break
		}
		node = node.Children[0]
	}
	return out
}

// walkDown returns [current, parent, grandparent, ..., trunk].
func walkDown(s *stack.Stack, from string) []string {
	var out []string
	name := from
	seen := map[string]bool{}
	for name != "" && !seen[name] {
		seen[name] = true
		out = append(out, name)
		node, ok := s.All[name]
		if !ok || node.Parent == "" {
			break
		}
		name = node.Parent
	}
	return out
}

func collectAll(s *stack.Stack) []string {
	var out []string
	for name := range s.All {
		out = append(out, name)
	}
	return out
}

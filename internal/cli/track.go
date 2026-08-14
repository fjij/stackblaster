package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/tui"
)

var trackParent string

var trackCmd = &cobra.Command{
	Use:   "track [--parent BRANCH]",
	Short: "Adopt the current branch into the stack under a parent branch",
	Long: `Sets branch.<current>.sbParent in git config so sb log/sync/restack
etc. can walk the stack. Pass --parent to specify the parent directly;
omit it in a TTY to pick from a list of local branches.`,
	RunE: runTrack,
}

var untrackCmd = &cobra.Command{
	Use:   "untrack",
	Short: "Forget the current branch's parent (leaves the git branch intact)",
	RunE:  runUntrack,
}

func init() {
	trackCmd.Flags().StringVar(&trackParent, "parent", "", "parent branch (opens a picker if omitted)")
	rootCmd.AddCommand(trackCmd, untrackCmd)
}

func runTrack(cmd *cobra.Command, args []string) error {
	ctx, err := loadContext()
	if err != nil {
		return err
	}
	if err := ctx.requireCurrent(); err != nil {
		return err
	}
	current, cfg := ctx.Current, ctx.Cfg
	if current == cfg.Trunk {
		return fmt.Errorf("refusing to track trunk (%s)", cfg.Trunk)
	}

	parent := trackParent
	if parent == "" {
		parent, err = pickParentForTrack(current, cfg.Trunk)
		if err != nil {
			return err
		}
		if parent == "" {
			return nil // user canceled the picker
		}
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

// pickParentForTrack shows a picker over every local branch except `current`.
// Returns "" (no error) if the user canceled.
func pickParentForTrack(current, trunk string) (string, error) {
	branches, err := gitx.ListBranches()
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(branches))
	items := make([]tui.PickerItem, 0, len(branches))
	for _, b := range branches {
		if b == current {
			continue
		}
		names = append(names, b)
		item := tui.PickerItem{Name: b}
		if b == trunk {
			item.Hint = "(trunk)"
			item.Current = true // start the cursor here — trunk is the most common target
		}
		items = append(items, item)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no other branches to track under")
	}
	notTTYErr := fmt.Errorf(
		"no parent given and this isn't a TTY — pass --parent BRANCH (e.g. --parent %s)",
		trunk,
	)
	return pickFromBranches(names, items, "Track "+current+" — pick a parent", notTTYErr)
}

func runUntrack(cmd *cobra.Command, args []string) error {
	ctx, err := loadContext()
	if err != nil {
		return err
	}
	if err := ctx.requireCurrent(); err != nil {
		return err
	}
	if err := gitx.UnsetConfig("branch." + ctx.Current + ".sbParent"); err != nil {
		return err
	}
	fmt.Printf("✓ untracked %s\n", ctx.Current)
	return nil
}

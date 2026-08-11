package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

// errNotImplemented is returned by commands that are scaffolded but not yet
// wired up. Keeping them registered means `sb --help` shows the full surface.
var errNotImplemented = errors.New("not implemented yet — this command is scaffolded but not wired up")

func stub(use, short string, aliases ...string) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		Aliases: aliases,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func init() {
	rootCmd.AddCommand(
		stub("modify", "Amend the current branch's commit and restack children"),
		stub("submit", "Push the stack and open/update PRs via `gh stack`"),
		stub("sync", "Fetch trunk, rebase the stack, prune merged branches"),
		stub("continue", "Resume after resolving a rebase conflict"),
		stub("checkout", "Interactive branch picker", "co"),
		stub("up", "Move up one branch in the stack"),
		stub("down", "Move down one branch in the stack"),
		stub("top", "Jump to the top of the current stack"),
		stub("bottom", "Jump to the bottom of the current stack (just above trunk)"),
		stub("track", "Adopt an existing branch into the stack"),
		stub("untrack", "Forget a branch (leaves the git branch intact)"),
		stub("restack", "Rebase children of the current branch onto it"),
	)
}

package cmd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
)

var createMsg string

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new branch stacked on the current branch",
	Long: `Create a new branch off the current branch, name it from the -m message
plus a configurable prefix and date, and commit any staged changes as the
first commit.

The parent branch is recorded in git config as branch.<name>.sbParent so that
sb log and sb sync know how the stack fits together.`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVarP(&createMsg, "message", "m", "", "commit message; also used as the branch slug")
	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	if err := gitx.Preflight(); err != nil {
		return err
	}
	repoRoot, err := gitx.RepoRoot()
	if err != nil {
		return fmt.Errorf("must be run inside a git repository")
	}
	cfg, err := config.Load(repoRoot)
	if err != nil {
		return err
	}
	if strings.TrimSpace(createMsg) == "" {
		return errors.New("-m/--message is required (interactive prompt coming soon)")
	}

	parent, err := gitx.CurrentBranch()
	if err != nil {
		return fmt.Errorf("resolve current branch: %w", err)
	}

	name := branchName(cfg.BranchPrefix, cfg.DateFormat, createMsg)

	if err := gitx.CheckoutNew(name); err != nil {
		return err
	}
	if err := gitx.SetConfig("branch."+name+".sbParent", parent); err != nil {
		return err
	}

	staged, err := gitx.HasStagedChanges()
	if err != nil {
		return err
	}
	if staged {
		count, _ := gitx.StagedFileCount()
		if err := gitx.Commit(createMsg); err != nil {
			return err
		}
		fmt.Printf("✓ Created %s (off %s)\n", name, parent)
		fmt.Printf("✓ Committed %d file(s)\n", count)
	} else {
		fmt.Printf("✓ Created %s (off %s) — no staged changes, branch is empty\n", name, parent)
		fmt.Println("  (stage changes with `git add`, then `sb modify`)")
	}
	return nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// branchName returns "<prefix>/<date>-<slug>". Prefix is optional.
func branchName(prefix, dateFmt, msg string) string {
	if dateFmt == "" {
		dateFmt = "2006-01-02"
	}
	date := time.Now().Format(dateFmt)
	slug := slugRe.ReplaceAllString(strings.ToLower(msg), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	base := date
	if slug != "" {
		base = date + "-" + slug
	}
	if prefix == "" {
		return base
	}
	return prefix + "/" + base
}

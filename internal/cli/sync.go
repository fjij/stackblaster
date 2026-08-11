package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var syncNoPrune bool

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Fetch trunk, rebase the stack onto it, prune merged branches",
	RunE:  runSync,
}

func init() {
	syncCmd.Flags().BoolVar(&syncNoPrune, "no-prune", false, "skip deleting local branches whose PRs have been merged")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
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
	current, _ := gitx.CurrentBranch()

	hasOrigin, err := gitx.RemoteExists("origin")
	if err != nil {
		return err
	}
	if hasOrigin {
		fmt.Println("↓ fetching origin…")
		if err := gitx.Fetch("origin"); err != nil {
			return err
		}
	} else {
		fmt.Println("(no `origin` remote — skipping fetch)")
	}

	// Capture trunk's pre-sync SHA so descendants know their old base.
	trunkOldSha, err := gitx.HeadSha(cfg.Trunk)
	if err != nil {
		return err
	}

	// Fast-forward trunk to origin/trunk if possible.
	if hasOrigin {
		remoteRef := "refs/remotes/origin/" + cfg.Trunk
		if _, err := gitx.RevParse(remoteRef); err == nil {
			if err := gitx.FastForward(cfg.Trunk, remoteRef); err != nil {
				return fmt.Errorf("fast-forward %s: %w", cfg.Trunk, err)
			}
		}
	}
	trunkNewSha, err := gitx.HeadSha(cfg.Trunk)
	if err != nil {
		return err
	}
	if trunkOldSha == trunkNewSha {
		fmt.Printf("= %s unchanged (%s)\n", cfg.Trunk, short(trunkNewSha))
	} else {
		fmt.Printf("✓ %s: %s → %s\n", cfg.Trunk, short(trunkOldSha), short(trunkNewSha))
	}

	// Restack every descendant of trunk.
	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}
	oldBaseFor := func(branch string) string {
		node := s.All[branch]
		if node == nil || node.Parent == "" {
			return ""
		}
		if node.Parent == cfg.Trunk {
			return trunkOldSha
		}
		sha, err := gitx.HeadSha(node.Parent)
		if err != nil {
			return ""
		}
		return sha
	}
	steps, err := stack.BuildPlanForChildren(s, cfg.Trunk, oldBaseFor)
	if err != nil {
		return err
	}
	if len(steps) > 0 {
		plan := &stack.Plan{
			Steps:       steps,
			ReturnTo:    current,
			Description: "restack tree after sync",
		}
		fmt.Printf("↻ restacking %d branch(es)…\n", len(steps))
		if err := plan.Execute(); err != nil {
			return err
		}
	}

	if !syncNoPrune && hasOrigin {
		return pruneMerged(s, cfg.Trunk, current)
	}
	return nil
}

// pruneMerged deletes local branches whose PRs have been merged on GitHub.
// Requires gh; skipped with a note if gh isn't installed or authenticated.
func pruneMerged(s *stack.Stack, trunk, current string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Println("(gh not installed — skipping merged-branch prune)")
		return nil
	}
	if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		fmt.Println("(gh not authenticated — skipping merged-branch prune)")
		return nil
	}
	var candidates []string
	for name := range s.All {
		if name == trunk {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return nil
	}
	merged, err := mergedBranches(candidates)
	if err != nil {
		fmt.Printf("(prune check failed: %v)\n", err)
		return nil
	}
	if len(merged) == 0 {
		return nil
	}
	for _, b := range merged {
		if b == current {
			fmt.Printf("(skipping delete of %s — it's checked out)\n", b)
			continue
		}
		if err := gitx.DeleteBranch(b); err != nil {
			fmt.Printf("(couldn't delete %s: %v)\n", b, err)
			continue
		}
		_ = gitx.UnsetConfig("branch." + b + ".sbParent")
		fmt.Printf("✂  pruned %s (PR merged)\n", b)
	}
	return nil
}

// mergedBranches returns the subset of `branches` whose PRs are in MERGED state
// on GitHub, according to `gh pr list`.
func mergedBranches(branches []string) ([]string, error) {
	out, err := exec.Command(
		"gh", "pr", "list",
		"--state", "merged",
		"--limit", "200",
		"--json", "headRefName",
		"--jq", ".[].headRefName",
	).Output()
	if err != nil {
		return nil, err
	}
	mergedSet := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			mergedSet[line] = true
		}
	}
	var result []string
	for _, b := range branches {
		if mergedSet[b] {
			result = append(result, b)
		}
	}
	return result, nil
}

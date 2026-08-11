package cli

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
	Short: "Fetch trunk, rebase the stack onto it, prune stale local branches",
	Long: `sb sync does three things:

  1. Fetch origin with --prune, so remote-tracking refs for branches that
     have been deleted upstream get cleaned up.
  2. Fast-forward trunk to origin/trunk, then restack every descendant.
  3. Delete local branches whose remote counterpart is gone (PR closed
     with branch deletion, PR merged with auto-delete, etc.) OR whose PR
     is currently in MERGED state on GitHub. Only sb-tracked branches
     (those with sbParent set) are eligible for pruning.

Pass --no-prune to skip step 3.`,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().BoolVar(&syncNoPrune, "no-prune", false, "skip deleting local branches that are gone from origin or merged")
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
		fmt.Println("↓ fetching origin (with --prune)…")
		if err := gitx.FetchPrune("origin"); err != nil {
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
		return pruneStale(s, cfg.Trunk, current)
	}
	return nil
}

// pruneStale deletes sb-tracked branches whose remote is gone or whose PR
// has been merged. "Gone from origin" is detected via git's upstream-track
// info (populated by the earlier fetch --prune) and doesn't require gh.
// The merged-PR check requires gh; if gh is missing or unauthed, only the
// gone-upstream half runs.
func pruneStale(s *stack.Stack, trunk, current string) error {
	// Tracked candidates only — never delete branches sb doesn't manage.
	tracked := make(map[string]bool, len(s.All))
	var candidates []string
	for name := range s.All {
		if name == trunk {
			continue
		}
		if _, err := gitx.GetConfig("branch." + name + ".sbParent"); err == nil {
			tracked[name] = true
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	toDelete := map[string]string{} // branch → reason

	// Gone-upstream: remote branch was deleted (PR closed w/ delete, merge
	// w/ auto-delete, or a manual delete on origin).
	gone, err := gitx.BranchesWithGoneUpstream()
	if err != nil {
		fmt.Printf("(couldn't list gone-upstream branches: %v)\n", err)
	}
	for _, b := range gone {
		if tracked[b] {
			toDelete[b] = "gone from origin"
		}
	}

	// Merged-PR: gh reports the branch as having a MERGED PR (branch might
	// still exist on origin if auto-delete is off).
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Println("(gh not installed — skipping merged-PR check)")
	} else if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		fmt.Println("(gh not authenticated — skipping merged-PR check)")
	} else {
		merged, err := mergedBranches(candidates)
		if err != nil {
			fmt.Printf("(merged-PR check failed: %v)\n", err)
		}
		for _, b := range merged {
			if _, already := toDelete[b]; !already {
				toDelete[b] = "PR merged"
			}
		}
	}

	if len(toDelete) == 0 {
		return nil
	}
	// If we're about to prune the currently checked-out branch, hop to trunk
	// first so the delete can proceed. Git won't let us delete the branch
	// we're on.
	if _, hitCurrent := toDelete[current]; hitCurrent {
		if err := gitx.Checkout(trunk); err != nil {
			return fmt.Errorf("couldn't check out %s before pruning current branch: %w", trunk, err)
		}
		fmt.Printf("↩ switched to %s to prune %s\n", trunk, current)
	}
	for b, reason := range toDelete {
		if err := gitx.DeleteBranch(b); err != nil {
			fmt.Printf("(couldn't delete %s: %v)\n", b, err)
			continue
		}
		_ = gitx.UnsetConfig("branch." + b + ".sbParent")
		_ = gitx.UnsetConfig("branch." + b + ".sbCreateMessage")
		fmt.Printf("✂  pruned %s (%s)\n", b, reason)
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

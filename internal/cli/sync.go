package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

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
  2. Fast-forward trunk to origin/trunk.
  3. Detect stale branches — local branches whose remote counterpart is
     gone OR whose PR is currently in MERGED state on GitHub. Only
     sb-tracked branches are eligible.
  4. Restack every remaining descendant. Detected-stale branches are
     skipped so their now-squashed commits aren't replayed onto the new
     trunk (which would conflict).
  5. Reparent children of stale branches to their surviving ancestor,
     then delete the stale branches.

Pass --no-prune to skip steps 3 and 5 (and restack everything).`,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().BoolVar(&syncNoPrune, "no-prune", false, "skip deleting local branches that are gone from origin or merged")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx, err := loadContext()
	if err != nil {
		return err
	}
	cfg, current := ctx.Cfg, ctx.Current

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

	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}

	// Detect doomed branches BEFORE restacking. If a merged branch is still
	// tracked locally, its commits are already squashed into trunk; rebasing
	// it onto the new trunk replays those same commits and conflicts. So we
	// figure out which branches are on the chopping block first and skip
	// them (and their subtrees) during restack.
	var doomed map[string]string
	if !syncNoPrune && hasOrigin {
		doomed = detectDoomed(s, cfg.Trunk)
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
	exclude := make(map[string]bool, len(doomed))
	for b := range doomed {
		exclude[b] = true
	}
	steps, err := stack.BuildPlanForChildren(s, cfg.Trunk, oldBaseFor, exclude)
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

	if !syncNoPrune && hasOrigin && len(doomed) > 0 {
		prunePlan := buildPrunePlan(s, cfg.Trunk, current, doomed)
		return executePrunePlan(prunePlan, cfg.Trunk, current)
	}
	return nil
}

// detectDoomed returns sb-tracked branches whose remote is gone or whose PR
// has been merged, keyed to a human-readable reason. "Gone from origin" is
// detected via git's upstream-track info (populated by the earlier fetch
// --prune) and doesn't require gh. The merged-PR check requires gh; if gh is
// missing or unauthed, only the gone-upstream half runs.
//
// Called before restack so sync can skip these branches when planning
// rebases — merged branches have already been squashed into trunk, and
// replaying their commits onto the new trunk would conflict.
func detectDoomed(s *stack.Stack, trunk string) map[string]string {
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

	gone, err := gitx.BranchesWithGoneUpstream()
	if err != nil {
		fmt.Printf("(couldn't list gone-upstream branches: %v)\n", err)
	}
	for _, b := range gone {
		if tracked[b] {
			toDelete[b] = "gone from origin"
		}
	}

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
	return toDelete
}

// executePrunePlan applies the reparents and deletions from a PrunePlan.
// Best-effort per step — individual failures are logged and skipped so a
// single stuck branch doesn't block the rest of the sweep.
func executePrunePlan(plan *PrunePlan, trunk, current string) error {
	for _, r := range plan.Reparents {
		if err := gitx.SetConfig("branch."+r.Branch+".sbParent", r.NewParent); err != nil {
			fmt.Printf("(couldn't reparent %s → %s: %v)\n", r.Branch, r.NewParent, err)
			continue
		}
		fmt.Printf("↔ reparented %s: %s → %s\n", r.Branch, r.OldParent, r.NewParent)
	}

	// If we're about to prune the currently checked-out branch, hop to trunk
	// first so the delete can proceed. Git won't let us delete the branch
	// we're on.
	if plan.HopToTrunk {
		if err := gitx.Checkout(trunk); err != nil {
			return fmt.Errorf("couldn't check out %s before pruning current branch: %w", trunk, err)
		}
		fmt.Printf("↩ switched to %s to prune %s\n", trunk, current)
	}
	for _, b := range plan.Delete {
		reason := plan.Doomed[b]
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

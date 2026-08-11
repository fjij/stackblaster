package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/ghx"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var (
	submitReady   bool
	submitDraft   bool
	submitDryRun  bool
)

var submitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Push the current stack and open/update PRs on GitHub",
	Long: `For each branch in the current stack (bottom to top, excluding trunk):
push with --force-with-lease, then create a PR if none exists, otherwise
update its base to match the branch's sbParent.

Drafts by default. Use --ready to open non-draft PRs, or --draft to force
draft even when config sets draft_by_default = false.`,
	RunE: runSubmit,
}

func init() {
	submitCmd.Flags().BoolVar(&submitReady, "ready", false, "open PRs as ready-for-review (overrides draft_by_default)")
	submitCmd.Flags().BoolVar(&submitDraft, "draft", false, "force draft (overrides --ready and draft_by_default = false)")
	submitCmd.Flags().BoolVar(&submitDryRun, "dry-run", false, "print what would happen without pushing or hitting GitHub")
	rootCmd.AddCommand(submitCmd)
}

func runSubmit(cmd *cobra.Command, args []string) error {
	if err := gitx.Preflight(); err != nil {
		return err
	}
	if !submitDryRun {
		if err := ghx.Preflight(); err != nil {
			return err
		}
	}
	repoRoot, err := gitx.RepoRoot()
	if err != nil {
		return errors.New("must be run inside a git repository")
	}
	cfg, err := config.Load(repoRoot)
	if err != nil {
		return err
	}
	current, err := gitx.CurrentBranch()
	if err != nil {
		return err
	}
	if current == cfg.Trunk {
		return fmt.Errorf("refusing to submit trunk (%s)", cfg.Trunk)
	}

	s, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}
	chain, err := chainToTrunk(s, current, cfg.Trunk)
	if err != nil {
		return err
	}
	if len(chain) == 0 {
		return fmt.Errorf("current branch %q is not tracked to trunk — run `sb track --parent %s`", current, cfg.Trunk)
	}

	// Bottom-to-top so parents exist before children reference them.
	draft := decideDraft(cfg, submitReady, submitDraft)
	for _, br := range chain {
		parent := s.All[br].Parent
		if err := submitBranch(br, parent, draft); err != nil {
			return err
		}
	}
	return nil
}

func decideDraft(cfg config.Config, ready, draft bool) bool {
	if draft {
		return true
	}
	if ready {
		return false
	}
	return cfg.DraftByDefault
}

// chainToTrunk returns branches from bottom (just above trunk) to top
// (current). Returns nil if the chain doesn't reach trunk.
func chainToTrunk(s *stack.Stack, current, trunk string) ([]string, error) {
	var reversed []string
	name := current
	seen := map[string]bool{}
	for name != "" && !seen[name] {
		seen[name] = true
		if name == trunk {
			// Reverse and return (excluding trunk).
			out := make([]string, len(reversed))
			for i, b := range reversed {
				out[len(reversed)-1-i] = b
			}
			return out, nil
		}
		reversed = append(reversed, name)
		node, ok := s.All[name]
		if !ok || node.Parent == "" {
			return nil, nil
		}
		name = node.Parent
	}
	return nil, nil
}

func submitBranch(branch, parent string, draft bool) error {
	if submitDryRun {
		fmt.Printf("• %s → PR base=%s draft=%v (dry-run)\n", branch, parent, draft)
		return nil
	}

	// Push with force-with-lease. If the branch has no upstream, this also
	// sets it.
	fmt.Printf("↑ pushing %s\n", branch)
	if err := gitx.PushForceWithLease("origin", branch); err != nil {
		return err
	}

	pr, err := ghx.PRForBranch(branch)
	if err != nil {
		return err
	}
	if pr == nil {
		title := prTitleFor(branch)
		body := prBodyFor(branch)
		url, err := ghx.CreatePR(ghx.CreatePROpts{
			Head:  branch,
			Base:  parent,
			Title: title,
			Body:  body,
			Draft: draft,
		})
		if err != nil {
			return err
		}
		fmt.Printf("✓ opened %s (base %s) — %s\n", branch, parent, url)
		return nil
	}
	// PR exists — retarget base if needed.
	if pr.BaseRef != parent {
		if err := ghx.SetPRBase(branch, parent); err != nil {
			return err
		}
		fmt.Printf("↻ retargeted #%d: base %s → %s\n", pr.Number, pr.BaseRef, parent)
	} else {
		fmt.Printf("= #%d up to date (base %s)\n", pr.Number, parent)
	}
	return nil
}

// prTitleFor derives a PR title from the branch's tip commit.
func prTitleFor(branch string) string {
	// We use the branch's HEAD commit subject.
	title, err := gitCommitSubject(branch)
	if err != nil || strings.TrimSpace(title) == "" {
		return branch
	}
	return title
}

func prBodyFor(branch string) string {
	body, _ := gitCommitBody(branch)
	return strings.TrimSpace(body)
}

func gitCommitSubject(rev string) (string, error) {
	return gitLogFormat(rev, "%s")
}

func gitCommitBody(rev string) (string, error) {
	return gitLogFormat(rev, "%b")
}

func gitLogFormat(rev, fmtStr string) (string, error) {
	out, err := exec.Command("git", "log", "-1", "--format="+fmtStr, rev).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/ghx"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var (
	submitReady    bool
	submitDraft    bool
	submitDryRun   bool
	submitTitle    string
	submitBodyFile string
	submitNoStack  bool
)

var submitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Push the current stack and open/update PRs on GitHub",
	Long: `For each branch in the current stack (bottom to top, excluding trunk):
push with --force-with-lease, then create a PR if none exists, otherwise
update its base to match the branch's sbParent.

If the stack has 2+ PRs, they're linked into a stack on GitHub via the
Stacked Pull Requests REST API so GitHub's UI shows the "Stacked pull
request" banner and stack navigation. Pass --no-stack to skip this step
(useful if the repo doesn't have stacked PRs enabled, or you just don't
want the linkage).

Drafts by default. Use --ready to open non-draft PRs, or --draft to force
draft even when config sets draft_by_default = false.

--title and --body-file override the PR title/body derived from the commit
message. They apply only to the *current* branch's PR when it's being
created for the first time; other branches in the stack use their commit-
derived title/body, and PRs that already exist aren't touched.`,
	RunE: runSubmit,
}

func init() {
	submitCmd.Flags().BoolVar(&submitReady, "ready", false, "open PRs as ready-for-review (overrides draft_by_default)")
	submitCmd.Flags().BoolVar(&submitDraft, "draft", false, "force draft (overrides --ready and draft_by_default = false)")
	submitCmd.Flags().BoolVar(&submitDryRun, "dry-run", false, "print what would happen without pushing or hitting GitHub")
	submitCmd.Flags().StringVar(&submitTitle, "title", "", "PR title for the current branch (default: commit subject)")
	submitCmd.Flags().StringVar(&submitBodyFile, "body-file", "", "read PR body for the current branch from this file (default: commit body)")
	submitCmd.Flags().BoolVar(&submitNoStack, "no-stack", false, "don't link the PRs into a stack on GitHub after creating them")
	rootCmd.AddCommand(submitCmd)
}

func runSubmit(cmd *cobra.Command, args []string) error {
	if err := gitx.Preflight(); err != nil {
		return err
	}
	// gh is optional: without it we still push, but skip PR creation,
	// retargeting, and stack linking. Users pushing to a non-GitHub remote
	// or without gh installed get a useful degraded mode instead of an
	// error.
	ghAvailable := true
	if !submitDryRun {
		if err := ghx.Preflight(); err != nil {
			ghAvailable = false
			fmt.Printf("(gh unavailable: %v)\n", err)
			fmt.Println("  branches will be pushed; PR creation and stack linking are skipped")
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

	// Optional per-invocation overrides for the current branch only.
	var titleOverride, bodyOverride string
	if submitTitle != "" {
		titleOverride = submitTitle
	}
	if submitBodyFile != "" {
		b, err := os.ReadFile(submitBodyFile)
		if err != nil {
			return fmt.Errorf("read --body-file: %w", err)
		}
		bodyOverride = string(b)
	}

	// Bottom-to-top so parents exist before children reference them.
	draft := decideDraft(cfg, submitReady, submitDraft)
	prNumbers := make([]int, 0, len(chain))
	for _, br := range chain {
		parent := s.All[br].Parent
		var titleFor, bodyFor string
		if br == current {
			titleFor, bodyFor = titleOverride, bodyOverride
		}
		num, err := submitBranch(br, parent, draft, titleFor, bodyFor, ghAvailable)
		if err != nil {
			return err
		}
		if num > 0 {
			prNumbers = append(prNumbers, num)
		}
	}

	// Link the PRs into a stack on GitHub. Best-effort: on any error we log
	// and continue — a failed link isn't worth abandoning already-created
	// PRs. Skip in dry-run, when --no-stack, and when gh isn't available.
	if !submitDryRun && !submitNoStack && ghAvailable && len(prNumbers) >= 2 {
		if err := linkStack(prNumbers); err != nil {
			fmt.Printf("(couldn't link PRs into a stack on GitHub: %v)\n", err)
		}
	}
	return nil
}

// linkStack registers the given PRs as a stack via the GitHub Stacked PRs REST
// API. If a stack containing any of these PRs already exists, only the new
// ones (in order) are appended.
func linkStack(prNumbers []int) error {
	owner, repo, err := ghx.RepoOwnerName()
	if err != nil {
		return err
	}
	// Look for an existing stack from the bottom PR upward, so we correctly
	// handle "some already linked, some new" cases.
	var existing *ghx.StackInfo
	for _, n := range prNumbers {
		s, err := ghx.StackForPR(owner, repo, n)
		if err != nil {
			return err
		}
		if s != nil {
			existing = s
			break
		}
	}
	if existing == nil {
		s, err := ghx.CreateStack(owner, repo, prNumbers)
		if err != nil {
			return err
		}
		fmt.Printf("🔗 linked stack #%d (%d PRs) on GitHub\n", s.Number, len(prNumbers))
		return nil
	}
	// Some PRs already in a stack — append the new ones.
	inStack := map[int]bool{}
	for _, n := range existing.PullRequests {
		inStack[n] = true
	}
	var missing []int
	for _, n := range prNumbers {
		if !inStack[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		fmt.Printf("= stack #%d already contains these %d PRs\n", existing.Number, len(prNumbers))
		return nil
	}
	s, err := ghx.AddToStack(owner, repo, existing.Number, missing)
	if err != nil {
		return err
	}
	fmt.Printf("🔗 added %d PR(s) to stack #%d on GitHub\n", len(missing), s.Number)
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

// submitBranch pushes the branch, then (if gh is available) creates or
// retargets its PR. Returns the PR's number, or 0 in dry-run / when gh is
// unavailable / when no PR is touched.
func submitBranch(branch, parent string, draft bool, titleOverride, bodyOverride string, ghAvailable bool) (int, error) {
	if submitDryRun {
		fmt.Printf("• %s → PR base=%s draft=%v (dry-run)\n", branch, parent, draft)
		return 0, nil
	}

	// Push with force-with-lease. If the branch has no upstream, this also
	// sets it.
	fmt.Printf("↑ pushing %s\n", branch)
	if err := gitx.PushForceWithLease("origin", branch); err != nil {
		return 0, err
	}

	if !ghAvailable {
		return 0, nil
	}

	pr, err := ghx.PRForBranch(branch)
	if err != nil {
		return 0, err
	}
	if pr == nil {
		title := titleOverride
		if title == "" {
			title = prTitleFor(branch)
		}
		body := bodyOverride
		if body == "" {
			body = prBodyFor(branch)
		}
		url, err := ghx.CreatePR(ghx.CreatePROpts{
			Head:  branch,
			Base:  parent,
			Title: title,
			Body:  body,
			Draft: draft,
		})
		if err != nil {
			return 0, err
		}
		fmt.Printf("✓ opened %s (base %s) — %s\n", branch, parent, url)
		// gh pr create doesn't return a number directly; look it up.
		if created, err := ghx.PRForBranch(branch); err == nil && created != nil {
			return created.Number, nil
		}
		return 0, nil
	}
	// PR exists — retarget base if needed.
	if pr.BaseRef != parent {
		if err := ghx.SetPRBase(branch, parent); err != nil {
			return pr.Number, err
		}
		fmt.Printf("↻ retargeted #%d: base %s → %s\n", pr.Number, pr.BaseRef, parent)
	} else {
		fmt.Printf("= #%d up to date (base %s)\n", pr.Number, parent)
	}
	return pr.Number, nil
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

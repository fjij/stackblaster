package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
	"github.com/fjij/stackblaster/internal/tui"
)

var (
	logAll      bool
	logNoStatus bool
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Print the full stack tree",
	Long: `Renders every tracked branch as a tree rooted at trunk. The current
branch is highlighted regardless of where you are — including trunk itself.

When a branch has multiple children, the child containing the current branch
stays on the main axis; siblings are shown as indented sub-tracks that
rejoin at a ├──┘ marker.

Each branch is annotated with its state:
  (needs restack) — the branch's sbParent has moved ahead but this branch
                    hasn't caught up. Run sb restack or sb sync.
  (needs submit)  — the branch has commits not on origin, or no upstream
                    yet. Run sb submit.

Pass --all to also list untracked and orphaned branches. Pass --no-status
to skip the state annotations (a bit faster on big trees).`,
	RunE: runLog,
}

func init() {
	logCmd.Flags().BoolVar(&logAll, "all", false, "also list untracked and orphaned branches")
	logCmd.Flags().BoolVar(&logNoStatus, "no-status", false, "skip needs-restack / needs-submit annotations")
	rootCmd.AddCommand(logCmd)
}

var (
	trunkStyle   = lipgloss.NewStyle().Foreground(tui.Trunk)
	branchStyle  = lipgloss.NewStyle().Foreground(tui.Branch)
	currentStyle = lipgloss.NewStyle().Foreground(tui.Accent).Bold(true)
	hintStyle    = lipgloss.NewStyle().Foreground(tui.Muted).Italic(true)
	warnStyle    = lipgloss.NewStyle().Foreground(tui.Accent).Italic(true)
	structStyle  = lipgloss.NewStyle().Foreground(tui.Muted)
)

// branchStatus captures freshness signals for a single branch. Zero value
// means "all good".
type branchStatus struct {
	needsRestack bool // sbParent has moved ahead but this branch hasn't caught up
	needsSubmit  bool // local branch has commits not on origin (or no upstream)
}

func runLog(cmd *cobra.Command, args []string) error {
	ctx, st, err := loadStackContext()
	if err != nil {
		return err
	}
	cfg, current := ctx.Cfg, ctx.Current

	// Precompute status for every tracked branch (except trunk). Cheap: a
	// couple git calls per branch, all local.
	statuses := map[string]branchStatus{}
	if !logNoStatus {
		for name, node := range st.All {
			if name == cfg.Trunk {
				continue
			}
			statuses[name] = computeBranchStatus(name, node.Parent)
		}
	}

	var b strings.Builder
	renderSubtree(&b, st.Trunk, "", true, current, statuses)

	if logAll {
		if len(st.Untracked) > 0 {
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, hintStyle.Render("untracked branches (no sbParent set):"))
			for _, n := range st.Untracked {
				fmt.Fprintf(&b, "  %s\n", n.Name)
			}
		}
		if len(st.Orphans) > 0 {
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, hintStyle.Render("orphaned (parent branch missing):"))
			for _, n := range st.Orphans {
				fmt.Fprintf(&b, "  %s → %s\n", n.Name, n.Parent)
			}
		}
	}
	fmt.Print(b.String())
	return nil
}

// computeBranchStatus checks whether `branch` is behind its sbParent (needs
// restack) and whether it has unsubmitted commits (needs submit). Any check
// that errors is skipped silently — a missing hint is fine, a wrong hint is
// worse.
//
// Two git subprocess calls total: one merge-base for restack, one
// for-each-ref for upstream tracking info.
func computeBranchStatus(branch, parent string) branchStatus {
	var s branchStatus
	if parent != "" {
		// If parent's tip is NOT an ancestor of branch, parent has commits
		// this branch doesn't include — restack needed.
		if ok, err := gitx.IsAncestor(parent, branch); err == nil && !ok {
			s.needsRestack = true
		}
	}
	upstream, track, err := gitx.BranchUpstream(branch)
	if err != nil {
		return s
	}
	if upstream == "" {
		s.needsSubmit = true // never pushed
	} else if strings.Contains(track, "ahead") || strings.Contains(track, "gone") {
		// [ahead N] → we have unpushed commits.
		// [gone]    → remote branch was deleted; a push would recreate it.
		// (Pure [behind N] means the remote is ahead of us; don't force submit.)
		s.needsSubmit = true
	}
	return s
}

// renderSubtree post-order-renders the subtree rooted at n, prepending
// `indent` to every line. When a node has multiple children, the child
// containing `current` (or the first alphabetically as a fallback) becomes
// the "primary" and stays on the current column; other children get indented
// by an extra "│  " level and rejoin via a `├──┘` marker back to this column.
func renderSubtree(b *strings.Builder, n *stack.Node, indent string, isRoot bool, current string, statuses map[string]branchStatus) {
	switch len(n.Children) {
	case 0:
		// leaf — no descendants to render.
	case 1:
		renderSubtree(b, n.Children[0], indent, false, current, statuses)
		fmt.Fprintln(b, indent+conn())
	default:
		primary, others := splitPrimary(n.Children, current)
		renderSubtree(b, primary, indent, false, current, statuses)
		fmt.Fprintln(b, indent+conn())

		subIndent := indent + structStyle.Render("│  ")
		// Each non-primary sibling gets its own opened-and-closed sub-track:
		// render the sibling's subtree at subIndent, then a rejoin marker,
		// then (if more siblings follow) trunk's │ continuing before the
		// next sub-track opens. This keeps adjacent siblings visually
		// distinct instead of stacking them like a linear chain.
		for i, c := range others {
			renderSubtree(b, c, subIndent, false, current, statuses)
			fmt.Fprintln(b, subIndent+conn())
			fmt.Fprintln(b, indent+structStyle.Render("├──┘"))
			if i < len(others)-1 {
				fmt.Fprintln(b, indent+conn())
			}
		}
	}

	// The node itself.
	fmt.Fprintln(b, indent+branchLine(n, current, isRoot, statuses[n.Name]))
}

// splitPrimary picks the child that leads to `current` as the primary (kept
// on the current axis). If no child contains `current`, the first child
// (alphabetical, already sorted by stack.Load) is used. Returns (primary,
// others).
func splitPrimary(children []*stack.Node, current string) (*stack.Node, []*stack.Node) {
	primaryIdx := 0
	for i, c := range children {
		if containsBranch(c, current) {
			primaryIdx = i
			break
		}
	}
	primary := children[primaryIdx]
	others := make([]*stack.Node, 0, len(children)-1)
	for i, c := range children {
		if i != primaryIdx {
			others = append(others, c)
		}
	}
	return primary, others
}

func containsBranch(n *stack.Node, name string) bool {
	if n.Name == name {
		return true
	}
	for _, c := range n.Children {
		if containsBranch(c, name) {
			return true
		}
	}
	return false
}

func conn() string {
	return structStyle.Render("│")
}

func branchLine(n *stack.Node, current string, isRoot bool, status branchStatus) string {
	marker := "●"
	nameStyle := branchStyle
	if isRoot {
		marker = "◇"
		nameStyle = trunkStyle
	}
	suffixes := []string{}
	if n.Name == current {
		marker = "◉"
		if !isRoot {
			nameStyle = currentStyle
		} else {
			nameStyle = currentStyle
		}
		suffixes = append(suffixes, hintStyle.Render("(current)"))
	}
	if status.needsRestack {
		suffixes = append(suffixes, warnStyle.Render("(needs restack)"))
	}
	if status.needsSubmit && !isRoot {
		suffixes = append(suffixes, warnStyle.Render("(needs submit)"))
	}
	line := marker + " " + nameStyle.Render(n.Name)
	if len(suffixes) > 0 {
		line += "  " + strings.Join(suffixes, " ")
	}
	return line
}

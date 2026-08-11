package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
	"github.com/fjij/stackblaster/internal/tui"
)

var logAll bool

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Print the full stack tree",
	Long: `Renders every tracked branch as a tree rooted at trunk. The current
branch is highlighted regardless of where you are — including trunk itself.

When a branch has multiple children, the child containing the current branch
stays on the main axis; siblings are shown as indented sub-tracks that
rejoin at a ├──┘ marker.

Pass --all to also list untracked and orphaned branches (branches whose
sbParent isn't set, or whose parent has been deleted) below the tree.`,
	RunE: runLog,
}

func init() {
	logCmd.Flags().BoolVar(&logAll, "all", false, "also list untracked and orphaned branches")
	rootCmd.AddCommand(logCmd)
}

var (
	trunkStyle   = lipgloss.NewStyle().Foreground(tui.Trunk)
	branchStyle  = lipgloss.NewStyle().Foreground(tui.Branch)
	currentStyle = lipgloss.NewStyle().Foreground(tui.Accent).Bold(true)
	hintStyle    = lipgloss.NewStyle().Foreground(tui.Muted).Italic(true)
	structStyle  = lipgloss.NewStyle().Foreground(tui.Muted)
)

func runLog(cmd *cobra.Command, args []string) error {
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
	st, err := stack.Load(cfg.Trunk)
	if err != nil {
		return err
	}
	current, _ := gitx.CurrentBranch()

	var b strings.Builder
	renderSubtree(&b, st.Trunk, "", true, current)

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

// renderSubtree post-order-renders the subtree rooted at n, prepending
// `indent` to every line. When a node has multiple children, the child
// containing `current` (or the first alphabetically as a fallback) becomes
// the "primary" and stays on the current column; other children get indented
// by an extra "│  " level and rejoin via a `├──┘` marker back to this column.
func renderSubtree(b *strings.Builder, n *stack.Node, indent string, isRoot bool, current string) {
	switch len(n.Children) {
	case 0:
		// leaf — no descendants to render.
	case 1:
		renderSubtree(b, n.Children[0], indent, false, current)
		fmt.Fprintln(b, indent+conn())
	default:
		primary, others := splitPrimary(n.Children, current)
		renderSubtree(b, primary, indent, false, current)
		fmt.Fprintln(b, indent+conn())

		subIndent := indent + structStyle.Render("│  ")
		// Each non-primary sibling gets its own opened-and-closed sub-track:
		// render the sibling's subtree at subIndent, then a rejoin marker,
		// then (if more siblings follow) trunk's │ continuing before the
		// next sub-track opens. This keeps adjacent siblings visually
		// distinct instead of stacking them like a linear chain.
		for i, c := range others {
			renderSubtree(b, c, subIndent, false, current)
			fmt.Fprintln(b, subIndent+conn())
			fmt.Fprintln(b, indent+structStyle.Render("├──┘"))
			if i < len(others)-1 {
				fmt.Fprintln(b, indent+conn())
			}
		}
	}

	// The node itself.
	fmt.Fprintln(b, indent+branchLine(n, current, isRoot))
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

func branchLine(n *stack.Node, current string, isRoot bool) string {
	if isRoot {
		if n.Name == current {
			return "◇ " + currentStyle.Render(n.Name) + "  " + hintStyle.Render("(current)")
		}
		return "◇ " + trunkStyle.Render(n.Name)
	}
	if n.Name == current {
		return "◉ " + currentStyle.Render(n.Name) + "  " + hintStyle.Render("(current)")
	}
	return "● " + branchStyle.Render(n.Name)
}

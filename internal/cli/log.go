package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

var logAll bool

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Print the stack tree (current stack by default)",
	RunE:  runLog,
}

func init() {
	logCmd.Flags().BoolVar(&logAll, "all", false, "show every tracked branch, not just the current stack")
	rootCmd.AddCommand(logCmd)
}

var (
	trunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	branchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	currentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	hintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
)

func connector() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("│")
}

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
	if logAll {
		renderTree(&b, st.Trunk, current)
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
	} else {
		renderCurrent(&b, st, current, cfg.Trunk)
	}
	fmt.Print(b.String())
	return nil
}

// renderCurrent walks from `current` up to trunk via sbParent links and prints
// each branch, current at top, trunk at bottom.
func renderCurrent(b *strings.Builder, s *stack.Stack, current, trunk string) {
	if current == "" {
		fmt.Fprintln(b, hintStyle.Render("detached HEAD — no stack context"))
		return
	}
	chain := []string{}
	seen := map[string]bool{}
	name := current
	for name != "" && !seen[name] {
		seen[name] = true
		chain = append(chain, name)
		if name == trunk {
			break
		}
		node, ok := s.All[name]
		if !ok || node.Parent == "" {
			break
		}
		name = node.Parent
	}
	for i, n := range chain {
		last := i == len(chain)-1
		if n == trunk {
			fmt.Fprintf(b, "◇ %s\n", trunkStyle.Render(n))
			continue
		}
		marker := "●"
		style := branchStyle
		suffix := ""
		if n == current {
			marker = "◉"
			style = currentStyle
			suffix = "  " + hintStyle.Render("(current)")
		}
		fmt.Fprintf(b, "%s %s%s\n", marker, style.Render(n), suffix)
		if !last {
			fmt.Fprintln(b, connector())
		}
	}
	if len(chain) > 0 && chain[len(chain)-1] != trunk {
		fmt.Fprintln(b, hintStyle.Render("  (not tracked to trunk — try `sb track --parent "+trunk+"`)"))
	}
}

// renderTree walks the full tree for --all view. Post-order: children first,
// then the node, so a linear stack reads leaf-at-top / trunk-at-bottom.
func renderTree(b *strings.Builder, n *stack.Node, current string) {
	renderNode(b, n, current, true)
}

func renderNode(b *strings.Builder, n *stack.Node, current string, isRoot bool) {
	for i, c := range n.Children {
		renderNode(b, c, current, false)
		if i < len(n.Children)-1 {
			fmt.Fprintln(b, hintStyle.Render("┊  (sibling)"))
		}
	}
	if len(n.Children) > 0 {
		fmt.Fprintln(b, connector())
	}
	if isRoot {
		fmt.Fprintf(b, "◇ %s\n", trunkStyle.Render(n.Name))
		return
	}
	marker := "●"
	style := branchStyle
	if n.Name == current {
		fmt.Fprintf(b, "◉ %s  %s\n", currentStyle.Render(n.Name), hintStyle.Render("(current)"))
		return
	}
	fmt.Fprintf(b, "%s %s\n", marker, style.Render(n.Name))
}

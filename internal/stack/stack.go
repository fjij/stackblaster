// Package stack models the stacked-branch tree. State lives in git config
// (branch.<name>.sbParent) so nothing outside git is authoritative.
package stack

import (
	"sort"
	"strings"

	"github.com/fjij/stackblaster/internal/gitx"
)

type Node struct {
	Name     string
	Parent   string
	Children []*Node
}

type Stack struct {
	Trunk    *Node
	All      map[string]*Node
	Orphans  []*Node // tracked branches whose parent isn't a known local branch
	Untracked []*Node // local branches without an sbParent (excluding trunk)
}

// Load builds the tree by iterating local branches and reading their sbParent.
// Uses one batched `git config --get-regexp` call for all sbParent entries
// instead of one call per branch, which matters when a repo has many tracked
// branches (nav commands felt sluggish before this).
func Load(trunk string) (*Stack, error) {
	branches, err := gitx.ListBranches()
	if err != nil {
		return nil, err
	}

	all := make(map[string]*Node, len(branches)+1)
	for _, b := range branches {
		all[b] = &Node{Name: b}
	}
	if _, ok := all[trunk]; !ok {
		all[trunk] = &Node{Name: trunk}
	}

	// One call for every branch's sbParent — git normalizes the variable
	// name to lowercase, so the map keys look like "branch.<name>.sbparent".
	sbparents, err := gitx.GetConfigRegexp(`^branch\..*\.sbparent$`)
	if err != nil {
		return nil, err
	}
	parentByBranch := make(map[string]string, len(sbparents))
	for key, value := range sbparents {
		if !strings.HasPrefix(key, "branch.") || !strings.HasSuffix(key, ".sbparent") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), ".sbparent")
		parentByBranch[name] = value
	}

	var orphans, untracked []*Node
	for _, b := range branches {
		if b == trunk {
			continue
		}
		parent := parentByBranch[b]
		if parent == "" {
			untracked = append(untracked, all[b])
			continue
		}
		all[b].Parent = parent
		p, ok := all[parent]
		if !ok {
			orphans = append(orphans, all[b])
			continue
		}
		p.Children = append(p.Children, all[b])
	}

	// Sort children for stable output.
	for _, n := range all {
		sort.Slice(n.Children, func(i, j int) bool {
			return n.Children[i].Name < n.Children[j].Name
		})
	}

	return &Stack{
		Trunk:     all[trunk],
		All:       all,
		Orphans:   orphans,
		Untracked: untracked,
	}, nil
}

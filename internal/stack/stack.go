// Package stack models the stacked-branch tree. State lives in git config
// (branch.<name>.sbParent) so nothing outside git is authoritative.
package stack

import (
	"sort"

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

	var orphans, untracked []*Node
	for _, b := range branches {
		if b == trunk {
			continue
		}
		parent, err := gitx.GetConfig("branch." + b + ".sbParent")
		if err != nil {
			return nil, err
		}
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

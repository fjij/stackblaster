package tui

import "github.com/charmbracelet/lipgloss"

// Shared color palette. Changed once here, applies app-wide.
//
// Accent is used for the "you are here" highlight in the stack tree
// and for the selected item in interactive pickers. Pancake yellow —
// picked to read as warm and appetizing on both dark and light terminals.
var (
	Accent = lipgloss.Color("#f4b942") // buttery pancake gold
	Muted  = lipgloss.Color("240")     // dark grey for hints, connectors
	Trunk  = lipgloss.Color("244")     // slightly brighter grey for trunk name
	Branch = lipgloss.Color("39")      // cool blue for non-current branches
)

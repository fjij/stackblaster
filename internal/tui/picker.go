// Package tui hosts interactive terminal UI bits (Bubble Tea).
package tui

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// PickerItem is a branch shown in the checkout picker.
type PickerItem struct {
	Name    string
	Hint    string // right-aligned annotation (e.g., "(current)")
	Current bool
}

// PickBranch shows a keyboard-driven list and returns the selected branch. If
// stdin isn't a TTY, PickBranch returns ErrNotATTY so the caller can fall back
// to something non-interactive.
func PickBranch(items []PickerItem, title string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", ErrNotATTY
	}
	if len(items) == 0 {
		return "", errors.New("no branches to pick from")
	}
	// Default cursor to the current branch, if any.
	initial := 0
	for i, it := range items {
		if it.Current {
			initial = i
			break
		}
	}
	m := pickerModel{items: items, cursor: initial, title: title}
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))
	res, err := p.Run()
	if err != nil {
		return "", err
	}
	final := res.(pickerModel)
	if final.canceled {
		return "", ErrCanceled
	}
	return final.items[final.cursor].Name, nil
}

// ErrNotATTY is returned by PickBranch when stdin isn't a terminal.
var ErrNotATTY = errors.New("not a TTY")

// ErrCanceled is returned by PickBranch when the user hits q/esc/ctrl-c.
var ErrCanceled = errors.New("canceled")

type pickerModel struct {
	items    []PickerItem
	cursor   int
	title    string
	canceled bool
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	itemStyle     = lipgloss.NewStyle().PaddingLeft(2)
	selectedStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(Accent).Bold(true)
	hintStyleTUI  = lipgloss.NewStyle().Foreground(Muted).Italic(true)
)

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = len(m.items) - 1
		}
	}
	return m, nil
}

func (m pickerModel) View() string {
	s := titleStyle.Render(m.title) + "\n\n"
	for i, it := range m.items {
		style := itemStyle
		marker := " "
		if i == m.cursor {
			style = selectedStyle
			marker = "›"
		}
		line := fmt.Sprintf("%s %s", marker, it.Name)
		if it.Hint != "" {
			line += "  " + hintStyleTUI.Render(it.Hint)
		}
		s += style.Render(line) + "\n"
	}
	s += "\n" + hintStyleTUI.Render("↑/↓ navigate · enter select · q cancel") + "\n"
	return s
}

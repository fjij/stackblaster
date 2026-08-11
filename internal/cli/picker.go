package cli

import (
	"errors"

	"github.com/fjij/stackblaster/internal/tui"
)

// pickFromBranches shows a Bubble Tea picker over `names` and returns the
// user's choice.
//
// Returns:
//   - the chosen branch and nil error on selection
//   - "" and nil error if the user canceled (q/esc/ctrl-c)
//   - "" and `notTTYErr` if stdin isn't a TTY (so scripts fail loudly
//     with a caller-appropriate hint instead of hanging on input)
//   - "" and the underlying error for anything else
//
// The pickerItems slice lets callers annotate branches with hints
// (e.g., "(trunk)", "(current parent)") and set the initial cursor.
// If items is nil, plain items are built from names.
func pickFromBranches(names []string, items []tui.PickerItem, title string, notTTYErr error) (string, error) {
	if items == nil {
		items = make([]tui.PickerItem, len(names))
		for i, n := range names {
			items[i] = tui.PickerItem{Name: n}
		}
	}
	choice, err := tui.PickBranch(items, title)
	if err == nil {
		return choice, nil
	}
	if errors.Is(err, tui.ErrCanceled) {
		return "", nil
	}
	if errors.Is(err, tui.ErrNotATTY) {
		return "", notTTYErr
	}
	return "", err
}

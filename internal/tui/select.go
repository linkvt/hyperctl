// Package tui adapts huh form fields.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Select returns the select field with the scroll position reset whenever
// the filter changes. huh keeps the previous scroll position, which hides
// matches above it even if all matches fit into the field.
func Select(s *huh.Select[string]) huh.Field {
	return &filterSelect{Select: s}
}

type filterSelect struct {
	*huh.Select[string]
}

// Update returns the wrapper, as huh groups replace a field with the model
// returned by its Update.
func (s *filterSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	wasFiltering := s.GetFiltering()
	_, cmd := s.Select.Update(msg)
	if keyMsg, ok := msg.(tea.KeyMsg); ok && wasFiltering && s.GetFiltering() && editsFilter(keyMsg) {
		s.scrollToTop()
	}
	return s, cmd
}

// scrollToTop moves the cursor and scroll position to the first match using
// the key bindings huh offers: leave the filter input, go to the first
// option and enter the filter input again, which keeps the filter.
func (s *filterSelect) scrollToTop() {
	if _, ok := s.Hovered(); !ok {
		// Leaving the filter input without matches clears the filter.
		return
	}
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyHome},
		{Type: tea.KeyRunes, Runes: []rune("/")},
	} {
		s.Select.Update(msg)
	}
}

func editsFilter(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete:
		return true
	default:
		return false
	}
}

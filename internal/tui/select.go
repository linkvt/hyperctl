// Package tui adapts huh form fields.
package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
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
func (s *filterSelect) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	wasFiltering := s.GetFiltering()
	_, cmd := s.Select.Update(msg)
	if _, ok := msg.(tea.PasteMsg); ok && s.GetFiltering() {
		// huh applies the filter only when it handles a key press.
		s.Select.Update(tea.KeyPressMsg{})
	}
	if wasFiltering && s.GetFiltering() && editsFilter(msg) {
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
	for _, msg := range []tea.KeyPressMsg{
		{Code: tea.KeyEscape},
		{Code: tea.KeyHome},
		{Code: '/', Text: "/"},
	} {
		s.Select.Update(msg)
	}
}

func editsFilter(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		return true
	case tea.KeyPressMsg:
		return msg.Text != "" || msg.Code == tea.KeyBackspace || msg.Code == tea.KeyDelete
	default:
		return false
	}
}

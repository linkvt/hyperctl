package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/linkvt/hyperctl/internal/tui"
)

func TestSelectShowsAllFilterMatchesAfterScrolling(t *testing.T) {
	s := newScrolledSelect()

	s.typeText("/5.1")

	view := s.model.View()
	for _, want := range []string{"5.1.0-0.ci", "5.1.0-0.nightly"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in view:\n%s", want, view)
		}
	}
}

func TestSelectKeepsFilterWhileTyping(t *testing.T) {
	s := newScrolledSelect()

	s.typeText("/5.1.0-0.n")

	view := s.model.View()
	if !strings.Contains(view, "5.1.0-0.nightly") || strings.Contains(view, "5.1.0-0.ci") {
		t.Errorf("expected only 5.1.0-0.nightly in view:\n%s", view)
	}
}

// scrolledSelect is a focused select field scrolled down by 20 options.
type scrolledSelect struct {
	model tea.Model
}

func newScrolledSelect() *scrolledSelect {
	var options []string
	for i := range 30 {
		options = append(options, fmt.Sprintf("4.%d.0-0.nightly", i+10))
	}
	options = append(options, "5.1.0-0.ci", "5.1.0-0.nightly")
	var value string
	field := tui.Select(huh.NewSelect[string]().Options(huh.NewOptions(options...)...).Height(10).Value(&value))
	field.WithKeyMap(huh.NewDefaultKeyMap())
	field.Focus()

	s := &scrolledSelect{model: field}
	for range 20 {
		s.send(tea.KeyMsg{Type: tea.KeyDown})
	}
	return s
}

func (s *scrolledSelect) typeText(text string) {
	for _, r := range text {
		s.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// send updates the field and renders it like bubbletea does, huh updates the
// viewport content while rendering.
func (s *scrolledSelect) send(msg tea.KeyMsg) {
	s.model, _ = s.model.Update(msg)
	s.model.View()
}

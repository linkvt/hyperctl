package app

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/huh/v2"
)

func TestNoteText(t *testing.T) {
	t.Setenv("TERM", "xterm")
	text := "quay.io/ci:rc_payload__5.1.0 ~/.ssh/id_ed25519.pub *.apps `x` C:\\dir"

	view := huh.NewNote().Description(noteText(text)).View()

	if got := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(view, ""); !strings.Contains(got, text) {
		t.Errorf("note shows:\n%s\nwant it to contain %q", got, text)
	}
}

func TestNoteTextInAccessibleMode(t *testing.T) {
	// huh runs forms in accessible mode with TERM=dumb, which prints the
	// description of a note as is.
	t.Setenv("TERM", "dumb")
	text := "quay.io/ci:rc_payload__5.1.0"

	if got := noteText(text); got != text {
		t.Errorf("got %q, want %q", got, text)
	}
}

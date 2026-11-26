package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileReplacesFile expects the file to be replaced by a complete
// one, without a temporary file left behind.
func TestWriteFileReplacesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	for _, content := range []string{"first\n", "second\n"} {
		if err := writeFile(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if got, err := os.ReadFile(path); err != nil || string(got) != "second\n" {
		t.Errorf("content: got %q, %v", got, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("permissions: got %v, %v", info, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("expected only the file, got %v", entries)
	}
}

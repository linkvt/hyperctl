package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// state is what hyperctl remembers between runs.
type state struct {
	// Current is the name of the cluster that commands act on unless
	// --cluster names one.
	Current     string `json:"current,omitempty"`
	LastRelease string `json:"lastRelease,omitempty"`
	// Setup is the last used setup.
	Setup setup `json:"setup,omitempty"`
	// LastRegions are the regions of the last created clusters, the default
	// unless the config file sets one.
	LastRegions lastRegions `json:"lastRegions,omitzero"`
}

// lastRegions holds the last used region per platform.
type lastRegions struct {
	AWS   string `json:"aws,omitempty"`
	Azure string `json:"azure,omitempty"`
}

func loadState(stateDir string) (state, error) {
	var s state
	_, err := readYAML(filepath.Join(stateDir, "state.yaml"), &s)
	return s, err
}

// updateState applies change to the saved state. It reads the state again
// instead of saving a copy loaded earlier, so that concurrent runs keep each
// other's changes.
func updateState(stateDir string, change func(*state)) error {
	s, err := loadState(stateDir)
	if err != nil {
		return err
	}
	change(&s)
	return writeYAML(filepath.Join(stateDir, "state.yaml"), s)
}

// readYAML reads path into v and reports whether the file exists.
func readYAML(path string, v any) (bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return true, nil
}

func writeYAML(path string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return writeFile(path, b)
}

// writeFile writes a private file, creating its parent directories.
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

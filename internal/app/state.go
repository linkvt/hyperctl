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
	MgmtCluster    string `json:"mgmtCluster,omitempty"`
	HostedCluster  string `json:"hostedCluster,omitempty"`
	LastRelease    string `json:"lastRelease,omitempty"`
	ManagementOnly bool   `json:"managementOnly,omitempty"`
}

// Platforms of the clusters.
const (
	platformAWS   = "aws"
	platformAzure = "azure"
)

func loadState(stateDir string) (state, error) {
	var s state
	_, err := readYAML(filepath.Join(stateDir, "state.yaml"), &s)
	return s, err
}

func saveState(stateDir string, s state) error {
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

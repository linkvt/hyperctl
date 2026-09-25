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
	MgmtCluster string `json:"mgmtCluster,omitempty"`
}

// clusterPhase is the lifecycle phase of a management cluster.
type clusterPhase string

const (
	phaseInstalling clusterPhase = "Installing"
	phaseInstalled  clusterPhase = "Installed"
)

// clusterRecord is what hyperctl remembers about a management cluster.
type clusterRecord struct {
	Phase clusterPhase `json:"phase"`
}

func loadState(stateDir string) (state, error) {
	var s state
	_, err := readYAML(filepath.Join(stateDir, "state.yaml"), &s)
	return s, err
}

func saveState(stateDir string, s state) error {
	return writeYAML(filepath.Join(stateDir, "state.yaml"), s)
}

// loadClusterRecord returns the record for a cluster and whether it exists.
func loadClusterRecord(clusterDir string) (clusterRecord, bool, error) {
	var r clusterRecord
	found, err := readYAML(filepath.Join(clusterDir, "cluster.yaml"), &r)
	return r, found, err
}

func saveClusterRecord(clusterDir string, r clusterRecord) error {
	return writeYAML(filepath.Join(clusterDir, "cluster.yaml"), r)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

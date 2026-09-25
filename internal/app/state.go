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
	MgmtCluster   string `json:"mgmtCluster,omitempty"`
	HostedCluster string `json:"hostedCluster,omitempty"`
	LastRelease   string `json:"lastRelease,omitempty"`
}

// clusterPhase is the lifecycle phase of a management cluster.
type clusterPhase string

const (
	phaseInstalling clusterPhase = "Installing"
	phaseInstalled  clusterPhase = "Installed"
)

// platformAWS is the only supported platform. Records carry the platform so
// that further platforms can be added without migrating them.
const platformAWS = "aws"

// clusterRecord is what hyperctl remembers about a management or hosted
// cluster, including what is needed to destroy it.
type clusterRecord struct {
	Platform   string       `json:"platform"`
	Phase      clusterPhase `json:"phase"`
	Release    string       `json:"release"`
	Region     string       `json:"region"`
	BaseDomain string       `json:"baseDomain"`
	// PullSecret is the pull secret path, needed to extract the installer
	// again for destroying a management cluster.
	PullSecret string `json:"pullSecret,omitempty"`
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
	return writeFile(path, b)
}

// writeFile writes a private file, creating its parent directories.
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

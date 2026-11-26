package app_test

import (
	"path/filepath"
	"testing"
)

func TestSelectionFailsForUnknownCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	for _, args := range [][]string{
		{"status", "--cluster", "missing"},
		{"kubeconfig", "--cluster", "missing"},
		{"image", "karpenter-operator", "--cluster", "missing", "quay.io/test/karpenter-operator:dev"},
		{"destroy", "--cluster", "missing", "--yes"},
	} {
		_, err := env.runErr(t, args...)

		if want := "cluster missing does not exist, clusters: test-mgmt, test-mgmt-workload"; err == nil || err.Error() != want {
			t.Errorf("%v: got %v, want %q", args, err, want)
		}
	}
}

func TestSelectionWithoutCurrentCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	writeFile(t, filepath.Join(env.home, ".local/state/hyperctl/state.yaml"), "lastRelease: 5.1.0-ec.0\n", 0o600)

	_, err := env.runErr(t, "status")

	if want := "no current cluster, select one with: hyperctl use <name>\nclusters: test-mgmt, test-mgmt-workload"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

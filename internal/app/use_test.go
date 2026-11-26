package app_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUseSetsCurrentCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "use", "test-mgmt-workload")

	if got := env.currentCluster(t); got != "test-mgmt-workload" {
		t.Errorf("current cluster: got %q, want test-mgmt-workload", got)
	}
}

func TestUseRejectsUnknownCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	_, err := env.runErr(t, "use", "missing")

	if want := "cluster missing does not exist, clusters: test-mgmt, test-mgmt-workload"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
	if got := env.currentCluster(t); got != "test-mgmt" {
		t.Errorf("current cluster: got %q, want it unchanged", got)
	}
}

func TestUseRequiresName(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "use")

	if err == nil || !strings.Contains(err.Error(), "expected the name of a cluster") {
		t.Errorf("expected a missing name error, got %v", err)
	}
}

// TestUseSelectsClusterWithoutRecord expects a cluster whose creation never
// started to be selectable.
func TestUseSelectsClusterWithoutRecord(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	mustMkdir(t, filepath.Join(env.home, ".local/share/hyperctl/clusters/test-new"))

	env.run(t, "use", "test-new")

	if got := env.currentCluster(t); got != "test-new" {
		t.Errorf("current cluster: got %q, want test-new", got)
	}
	if out := env.run(t, "status"); !strings.Contains(out, "Cluster test-new\n") {
		t.Errorf("expected the status of test-new, got:\n%s", out)
	}
	env.run(t, "destroy", "--yes")
	if got := env.currentCluster(t); got != "" {
		t.Errorf("current cluster after destroy: got %q, want none", got)
	}
	if got := len(env.images.destroys) + len(env.hypershift.destroys); got != 0 {
		t.Errorf("expected nothing destroyed in the cloud, got %d", got)
	}
}

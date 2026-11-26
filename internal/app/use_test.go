package app_test

import (
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

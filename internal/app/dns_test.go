package app_test

import (
	"strings"
	"testing"
)

func TestCreateDeletesDNSLeftoversOfUntrackedClusters(t *testing.T) {
	env := newTestEnv(t)
	env.aws.dns["test-mgmt"] = []string{"A api.test-mgmt.example.com."}
	env.aws.dns["test-mgmt-workload"] = []string{"private zone test-mgmt-workload.hypershift.local."}
	env.aws.dns["unrelated"] = []string{"A api.unrelated.example.com."}

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--yes",
	)

	for _, name := range []string{"test-mgmt", "test-mgmt-workload"} {
		if entries := env.aws.dns[name]; len(entries) != 0 {
			t.Errorf("expected DNS leftovers of %s to be deleted, got %v", name, entries)
		}
	}
	if len(env.aws.dns["unrelated"]) != 1 {
		t.Errorf("expected DNS entries of other clusters to be kept, got %v", env.aws.dns)
	}
}

func TestCreateRequiresConfirmationForDNSLeftovers(t *testing.T) {
	env := newTestEnv(t)
	env.aws.dns["test-mgmt"] = []string{"A api.test-mgmt.example.com."}

	_, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if err == nil || !strings.Contains(err.Error(), "A api.test-mgmt.example.com.") || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("expected error listing DNS leftovers and --yes, got %v", err)
	}
	if got := len(env.installerCalls(t)); got != 0 {
		t.Errorf("expected no installer calls, got %d", got)
	}
}

func TestCreateKeepsDNSOfTrackedClusters(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.aws.dns["test-mgmt"] = []string{"A api.test-mgmt.example.com."}
	env.aws.dns["test-mgmt-workload"] = []string{"private zone test-mgmt-workload.hypershift.local."}

	env.run(t, "create", "--yes")

	if len(env.aws.dns["test-mgmt"]) != 1 || len(env.aws.dns["test-mgmt-workload"]) != 1 {
		t.Errorf("expected DNS entries of tracked clusters to be kept, got %v", env.aws.dns)
	}
}

func TestDestroyDeletesDNSLeftovers(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.aws.dns["test-mgmt"] = []string{"A api.test-mgmt.example.com."}
	env.aws.dns["test-mgmt-workload"] = []string{"private zone test-mgmt-workload.hypershift.local."}

	env.run(t, "destroy", "--yes")

	for _, name := range []string{"test-mgmt", "test-mgmt-workload"} {
		if entries := env.aws.dns[name]; len(entries) != 0 {
			t.Errorf("expected DNS leftovers of %s to be deleted, got %v", name, entries)
		}
	}
}

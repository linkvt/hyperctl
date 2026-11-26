package app_test

import (
	"path/filepath"
	"testing"
)

func TestList(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	// list reads the records only.
	env.aws, env.azure, env.cluster = nil, nil, nil

	out := env.run(t, "list")

	want := "" +
		"  NAME                   PLATFORM  SETUP       PHASE\n" +
		"* test-mgmt              aws       management  Installed\n" +
		"  └─ test-mgmt-workload                        Installed\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestListShowsClustersNotCreated(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.runErr(t, "create", "--setup", "standalone",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--ssh-key", filepath.Join(env.home, "missing.pub"),
	); err == nil {
		t.Fatal("expected create to fail without the SSH key")
	}

	out := env.run(t, "list")

	want := "" +
		"  NAME       PLATFORM  SETUP  PHASE\n" +
		"* test-mgmt                   not created\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestListWithoutClusters(t *testing.T) {
	env := newTestEnv(t)

	out := env.run(t, "list")

	if want := "No clusters, create one with: hyperctl create\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

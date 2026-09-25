package app_test

import (
	"os"
	"strings"
	"testing"
)

func TestKubeconfigMgmtPrintsInstallerKubeconfigPath(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	path := strings.TrimSpace(env.run(t, "kubeconfig", "mgmt"))

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading printed kubeconfig path %q: %v", path, err)
	}
	if strings.TrimSpace(string(content)) != "fake kubeconfig" {
		t.Errorf("printed path %q is not the installer kubeconfig, content: %q", path, content)
	}
}

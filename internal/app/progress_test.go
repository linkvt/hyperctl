package app_test

import (
	"strings"
	"testing"
)

func TestCreateReportsProgress(t *testing.T) {
	env := newTestEnv(t)

	out := env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	for _, want := range []string{
		"✓ AWS identity " + testIAMUserARN,
		"✓ EC2 Spot service-linked role exists",
		"✓ Created OIDC bucket tester-hypershift-oidc in eu-central-1",
		"✓ No DNS leftovers",
		"✓ Extracted openshift-install 5.1.0-ec.0",
		"  [openshift-install] level=info msg=fake install\n",
		"✓ Installed management cluster test-mgmt",
		"  [hypershift] fake install output\n",
		"✓ Installed the HyperShift operator",
		"  [hypershift] fake create output\n",
		"✓ Created hosted cluster test-mgmt-workload",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestCreateReportsCachedInstaller(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.run(t, "destroy", "--yes")

	out := env.run(t, "create", "--name", "second", "--region", "eu-central-1", "--pull-secret", env.pullSecret)

	if want := "✓ Using cached openshift-install 5.1.0-ec.0"; !strings.Contains(out, want) {
		t.Errorf("expected %q in output:\n%s", want, out)
	}
}

package app_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStatus(t *testing.T) {
	tests := map[string]struct {
		apiDown         bool
		hostedAvailable string
		want            []string
	}{
		"available clusters": {
			hostedAvailable: "True",
			want: []string{
				`(?m)^Management cluster test-mgmt$`,
				`(?m)^  Phase:\s+Installed \(since \d{4}-\d\d-\d\d \d\d:\d\d, 0s ago\)$`,
				`(?m)^  Release:\s+5\.1\.0-ec\.0$`,
				`(?m)^  Region:\s+eu-central-1$`,
				`(?m)^  HyperShift:\s+installed$`,
				`(?m)^  API:\s+reachable$`,
				`(?m)^  Kubeconfig:\s+\S+/clusters/test-mgmt/install/auth/kubeconfig$`,
				`(?m)^Hosted cluster test-mgmt-workload$`,
				`(?m)^  Available:\s+True$`,
				`(?m)^  Kubeconfig:\s+\S+/clusters/test-mgmt/hosted/test-mgmt-workload/kubeconfig$`,
			},
		},
		"unavailable hosted cluster": {
			hostedAvailable: "False",
			want:            []string{`(?m)^  Available:\s+False$`},
		},
		"unreachable management cluster": {
			apiDown: true,
			want: []string{
				`(?m)^  API:\s+unreachable, the instances may have been pruned, recreate the cluster with: hyperctl destroy --cluster test-mgmt && hyperctl create --name test-mgmt$`,
				`(?m)^  Available:\s+unknown$`,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)
			if tc.apiDown {
				env.cluster.apiDown = true
			}
			env.cluster.hostedClusters["test-mgmt-workload"].available = tc.hostedAvailable

			out := env.run(t, "status")

			for _, want := range tc.want {
				if !regexp.MustCompile(want).MatchString(out) {
					t.Errorf("expected output to match %s, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestStatusShowsClusterWithoutOperator(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create", "--platform", "azure", "--setup", "standalone", "--region", testAzureLocation, "--name", "test-aks")

	out := env.run(t, "status")

	for _, want := range []string{
		`(?m)^Cluster test-aks$`,
		`(?m)^  HyperShift:\s+not installed$`,
	} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("expected output to match %s, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Release:") {
		t.Errorf("expected no release for an AKS cluster without operator, got:\n%s", out)
	}
}

// TestStatusShowsStandaloneClusterNotCreated expects a standalone cluster
// whose creation failed before it was recorded to be called a cluster.
func TestStatusShowsStandaloneClusterNotCreated(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.runErr(t, "create", "--setup", "standalone", "--yes",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--ssh-key", filepath.Join(env.home, "missing.pub"),
	); err == nil {
		t.Fatal("expected create to fail without the SSH key")
	}
	out := env.run(t, "status")

	for _, want := range []string{
		`(?m)^Cluster test-mgmt$`,
		`(?m)^  Phase:\s+not created$`,
	} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("expected output to match %s, got:\n%s", want, out)
		}
	}
}

// TestStatusShowsOperatorOfEarlierVersions expects management clusters
// recorded by earlier versions to have the HyperShift operator, as every
// earlier create installed it.
func TestStatusShowsOperatorOfEarlierVersions(t *testing.T) {
	env := newTestEnv(t)
	if err := os.CopyFS(filepath.Join(env.home, ".local/share/hyperctl"), os.DirFS(filepath.Join("testdata", "data"))); err != nil {
		t.Fatal(err)
	}

	out := env.run(t, "status", "--cluster", "aws-mgmt")

	for _, want := range []string{
		`(?m)^Management cluster aws-mgmt$`,
		`(?m)^  HyperShift:\s+installed$`,
	} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("expected output to match %s, got:\n%s", want, out)
		}
	}
}
func TestStatusShowsIncompleteInstallation(t *testing.T) {
	env := newTestEnv(t)
	env.images.installErr = errors.New("openshift-install create cluster: exit status 1")
	if _, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	); err == nil {
		t.Fatal("expected failing installer to fail create")
	}

	out := env.run(t, "status")

	for _, want := range []string{
		`(?m)^  Phase:\s+Installing \(since .*\)$`,
		`(?m)^  Note:\s+unless hyperctl create is still running, the installation failed, destroy it with: hyperctl destroy --cluster test-mgmt$`,
		`(?m)^  Log:\s+\S*` + regexp.QuoteMeta(filepath.Join("clusters", "test-mgmt", "install", ".openshift_install.log")) + `$`,
	} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("expected output to match %s, got:\n%s", want, out)
		}
	}
}

func TestStatusShowsHostedOnlyCleanupForIncompleteHostedInstallation(t *testing.T) {
	env := newTestEnv(t)
	env.hypershift.createClusterErr = errors.New("hypershift create cluster: exit status 1")
	if _, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	); err == nil {
		t.Fatal("expected failing hosted cluster creation to fail create")
	}

	out := env.run(t, "status")
	want := `(?m)^  Note:\s+unless hyperctl create is still running, the installation failed, destroy it with: hyperctl destroy --cluster test-mgmt-workload$`
	if !regexp.MustCompile(want).MatchString(out) {
		t.Errorf("expected output to match %s, got:\n%s", want, out)
	}
}

func TestStatusWithoutClusters(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "status")

	if want := "no clusters, create one with: hyperctl create"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

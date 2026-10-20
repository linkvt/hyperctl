package app_test

import (
	"path/filepath"
	"regexp"
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
				`(?m)^  API:\s+unreachable, the instances may have been pruned, recreate the clusters with: hyperctl destroy && hyperctl create$`,
				`(?m)^  Available:\s+unknown$`,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)
			if tc.apiDown {
				t.Setenv("HYPERCTL_TEST_API_DOWN", "1")
			}
			t.Setenv("HYPERCTL_TEST_HOSTED_AVAILABLE", tc.hostedAvailable)

			out := env.run(t, "status")

			for _, want := range tc.want {
				if !regexp.MustCompile(want).MatchString(out) {
					t.Errorf("expected output to match %s, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestStatusShowsIncompleteInstallation(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("HYPERCTL_TEST_INSTALLER_FAIL", "1")
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
		`(?m)^  Note:\s+unless hyperctl create is still running, the installation failed, destroy it with: hyperctl destroy$`,
		`(?m)^  Log:\s+\S*` + regexp.QuoteMeta(filepath.Join("clusters", "test-mgmt", "install", ".openshift_install.log")) + `$`,
	} {
		if !regexp.MustCompile(want).MatchString(out) {
			t.Errorf("expected output to match %s, got:\n%s", want, out)
		}
	}
}

func TestStatusShowsHostedOnlyCleanupForIncompleteHostedInstallation(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("HYPERCTL_TEST_HYPERSHIFT_FAIL", "create")
	if _, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	); err == nil {
		t.Fatal("expected failing hosted cluster creation to fail create")
	}

	out := env.run(t, "status")
	want := `(?m)^  Note:\s+unless hyperctl create is still running, the installation failed, destroy it with: hyperctl destroy hosted$`
	if !regexp.MustCompile(want).MatchString(out) {
		t.Errorf("expected output to match %s, got:\n%s", want, out)
	}
}

func TestStatusWithoutClusters(t *testing.T) {
	env := newTestEnv(t)

	out := env.run(t, "status")

	if want := "No clusters, create them with: hyperctl create\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

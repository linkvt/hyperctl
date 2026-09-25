package app_test

import (
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
				"Management cluster test-mgmt: Installed, release 5.1.0-ec.0, region eu-central-1, API reachable",
				"Hosted cluster test-mgmt-workload: Installed, Available=True",
			},
		},
		"unavailable hosted cluster": {
			hostedAvailable: "False",
			want:            []string{"Hosted cluster test-mgmt-workload: Installed, Available=False"},
		},
		"unreachable management cluster": {
			apiDown: true,
			want: []string{
				"Management cluster test-mgmt: Installed, release 5.1.0-ec.0, region eu-central-1, API unreachable",
				"hyperctl destroy",
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
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q, got:\n%s", want, out)
				}
			}
		})
	}
}

func TestStatusWithoutClusters(t *testing.T) {
	env := newTestEnv(t)

	out := env.run(t, "status")

	if !strings.Contains(out, "No clusters") {
		t.Errorf("expected no clusters, got:\n%s", out)
	}
}

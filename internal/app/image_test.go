package app_test

import (
	"regexp"
	"strings"
	"testing"
)

func TestImageSetsOverride(t *testing.T) {
	tests := map[string]struct {
		component string
		want      string
	}{
		"karpenter operator": {
			component: "karpenter-operator",
			want:      `(?m)^  Image overrides:\n    Karpenter operator:\s+quay.io/test/karpenter-operator:dev$`,
		},
		"karpenter provider": {
			component: "karpenter-provider",
			want:      `(?m)^  Image overrides:\n    Karpenter provider:\s+quay.io/test/karpenter-operator:dev$`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)

			out := env.run(t, "image", tc.component, "quay.io/test/karpenter-operator:dev")

			if want := "rolls out"; !strings.Contains(out, want) {
				t.Errorf("expected %q in output:\n%s", want, out)
			}
			if status := env.run(t, "status"); !regexp.MustCompile(tc.want).MatchString(status) {
				t.Errorf("expected status to match %s:\n%s", tc.want, status)
			}
			if calls := env.ocCalls(t, "delete pod"); len(calls) != 0 {
				t.Errorf("expected no pod deletion for a changed image, got %v", calls)
			}
		})
	}
}

func TestImageRestartsPodsForUnchangedImage(t *testing.T) {
	tests := map[string]struct {
		image       string
		wantWarning bool
	}{
		"latest tag": {image: "quay.io/test/karpenter-operator:latest"},
		"other tag":  {image: "quay.io/test/karpenter-operator:dev", wantWarning: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)
			env.run(t, "image", "karpenter-operator", tc.image)

			out := env.run(t, "image", "karpenter-operator", tc.image)

			calls := env.ocCalls(t, "delete pod")
			if want := "delete pod -n clusters-test-mgmt-workload -l app=karpenter-operator"; len(calls) != 1 || !strings.Contains(calls[0], want) {
				t.Errorf("expected pod deletion %q, got %v", want, calls)
			}
			if gotWarning := strings.Contains(out, "IfNotPresent"); gotWarning != tc.wantWarning {
				t.Errorf("expected warning %v, got output:\n%s", tc.wantWarning, out)
			}
		})
	}
}

func TestImageResetRemovesOverride(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.run(t, "image", "karpenter-operator", "quay.io/test/karpenter-operator:dev")

	env.run(t, "image", "karpenter-operator", "--reset")

	if status := env.run(t, "status"); strings.Contains(status, "Image overrides") {
		t.Errorf("expected no override in status:\n%s", status)
	}
}

func TestImageRequiresHostedCluster(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "image", "karpenter-operator", "quay.io/test/karpenter-operator:dev")

	if err == nil || !strings.Contains(err.Error(), "no hosted cluster") {
		t.Errorf("expected missing hosted cluster error, got %v", err)
	}
}

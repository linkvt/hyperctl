package app_test

import (
	"regexp"
	"strings"
	"testing"
)

// providerImageAnnotation overrides the Karpenter provider image on the
// HostedControlPlane of testControlPlane.
const providerImageAnnotation = "autoscaling.openshift.io/karpenter-image"

var testControlPlane = fakeObject{kind: "hostedcontrolplane", namespace: "clusters-test-mgmt-workload", name: "test-mgmt-workload"}

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
			if pods := env.cluster.deletedPods; len(pods) != 0 {
				t.Errorf("expected no pod deletion for a changed image, got %+v", pods)
			}
		})
	}
}

func TestImageSetsProviderOverride(t *testing.T) {
	const image = "quay.io/test/karpenter-aws:dev"
	tests := map[string]struct {
		azure bool
	}{
		// The standalone karpenter-operator deploys the provider itself and
		// reads the override from the HostedControlPlane.
		"standalone operator": {},
		// Azure requires the standalone karpenter-operator and has no
		// HostedCluster annotation for the provider image.
		"azure": {azure: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			if tc.azure {
				env.createAzureClusters(t)
			} else {
				env.createClusters(t)
			}

			out := env.run(t, "image", "karpenter-provider", image)

			if want := "rolls out"; !strings.Contains(out, want) {
				t.Errorf("expected %q in output:\n%s", want, out)
			}
			if got := env.cluster.annotations[testControlPlane][providerImageAnnotation]; got != image {
				t.Errorf("provider image annotation of the HostedControlPlane: got %q, want %q", got, image)
			}
			wantStatus := "(?m)^  Image overrides:\n    Karpenter provider:\\s+" + regexp.QuoteMeta(image) + "$"
			if status := env.run(t, "status"); !regexp.MustCompile(wantStatus).MatchString(status) {
				t.Errorf("expected status to match %s:\n%s", wantStatus, status)
			}
		})
	}
}

func TestImageRestartsPodsForUnchangedImage(t *testing.T) {
	tests := map[string]struct {
		component string
		image     string
		want      fakePods
	}{
		"karpenter operator": {
			component: "karpenter-operator",
			image:     "quay.io/test/karpenter-operator:latest",
			want:      fakePods{namespace: "clusters-test-mgmt-workload", selector: "app=karpenter-operator"},
		},
		"karpenter provider": {
			component: "karpenter-provider",
			image:     "quay.io/test/karpenter-aws:dev",
			want:      fakePods{namespace: "clusters-test-mgmt-workload", selector: "app=karpenter"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)
			env.run(t, "image", tc.component, tc.image)

			out := env.run(t, "image", tc.component, tc.image)

			if pods := env.cluster.deletedPods; len(pods) != 1 || pods[0] != tc.want {
				t.Errorf("expected pod deletion %+v, got %+v", tc.want, pods)
			}
			if want := "unique tag"; !strings.Contains(out, want) {
				t.Errorf("expected %q in output:\n%s", want, out)
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

func TestImageResetRemovesProviderOverride(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.run(t, "image", "karpenter-provider", "quay.io/test/karpenter-aws:dev")

	env.run(t, "image", "karpenter-provider", "--reset")

	if _, set := env.cluster.annotations[testControlPlane][providerImageAnnotation]; set {
		t.Errorf("expected the provider image annotation to be removed, got %v", env.cluster.annotations[testControlPlane])
	}
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

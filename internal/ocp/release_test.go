package ocp_test

import (
	"testing"

	"github.com/linkvt/hyperctl/internal/ocp"
)

func TestParseRelease(t *testing.T) {
	tests := map[string]struct {
		ref  string
		want ocp.Release
	}{
		"version": {
			ref: "5.1.0-ec.0",
			want: ocp.Release{
				Name:     "5.1.0-ec.0",
				PullSpec: "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
			},
		},
		"quay pull spec": {
			ref: "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
			want: ocp.Release{
				Name:     "5.1.0-ec.0",
				PullSpec: "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
			},
		},
		"nightly pull spec": {
			ref: "registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-25-074941",
			want: ocp.Release{
				Name:     "5.1.0-0.nightly-2026-09-25-074941",
				PullSpec: "registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-25-074941",
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ocp.ParseRelease(tc.ref); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReleaseRegistry(t *testing.T) {
	tests := map[string]struct {
		ref  string
		want string
	}{
		"version": {ref: "5.1.0-ec.0", want: "quay.io"},
		"nightly": {ref: "registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly", want: "registry.ci.openshift.org"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ocp.ParseRelease(tt.ref).Registry(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

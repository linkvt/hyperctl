package hypershift_test

import (
	"regexp"
	"testing"

	"github.com/linkvt/hyperctl/internal/hypershift"
)

func TestNewInfraID(t *testing.T) {
	tests := map[string]struct {
		name string
		want string
	}{
		"short name": {
			name: "test-workload",
			want: `^test-workload-[a-z0-9]{5}$`,
		},
		// hypershift created vlink-karpenter-workl-vgg9h for this name.
		"long name is shortened": {
			name: "vlink-karpenter-workload",
			want: `^vlink-karpenter-workl-[a-z0-9]{5}$`,
		},
		"shortened name does not end with a dash": {
			name: "vlink-karpenter-work-load",
			want: `^vlink-karpenter-work-[a-z0-9]{5}$`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := hypershift.NewInfraID(tc.name); !regexp.MustCompile(tc.want).MatchString(got) {
				t.Errorf("got %q, want match of %s", got, tc.want)
			}
		})
	}
}

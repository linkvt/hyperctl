package ocp_test

import (
	"testing"

	"github.com/linkvt/hyperctl/internal/ocp"
)

func TestRenderInstallConfig(t *testing.T) {
	tests := map[string]struct {
		params ocp.InstallConfigParams
		want   string
	}{
		"without SSH key": {
			params: ocp.InstallConfigParams{
				Name:       "test-mgmt",
				BaseDomain: "example.com",
				Region:     "eu-central-1",
				PullSecret: `{"auths":{}}`,
			},
			want: `apiVersion: v1
baseDomain: example.com
metadata:
  name: test-mgmt
platform:
  aws:
    region: eu-central-1
pullSecret: '{"auths":{}}'
`,
		},
		"with SSH key": {
			params: ocp.InstallConfigParams{
				Name:       "test-mgmt",
				BaseDomain: "example.com",
				Region:     "eu-central-1",
				PullSecret: `{"auths":{}}`,
				SSHKey:     "ssh-ed25519 AAAA test@example.com",
			},
			want: `apiVersion: v1
baseDomain: example.com
metadata:
  name: test-mgmt
platform:
  aws:
    region: eu-central-1
pullSecret: '{"auths":{}}'
sshKey: ssh-ed25519 AAAA test@example.com
`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ocp.RenderInstallConfig(tc.params)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

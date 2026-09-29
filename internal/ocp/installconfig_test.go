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
		"compact in a single zone": {
			params: ocp.InstallConfigParams{
				Name:         "test-mgmt",
				BaseDomain:   "example.com",
				Region:       "eu-central-1",
				Zone:         "eu-central-1b",
				InstanceType: "t3a.xlarge",
				Workers:      0,
				PullSecret:   `{"auths":{}}`,
			},
			want: `apiVersion: v1
baseDomain: example.com
compute:
- name: worker
  platform:
    aws:
      type: t3a.xlarge
  replicas: 0
controlPlane:
  name: master
  platform:
    aws:
      type: t3a.xlarge
  replicas: 3
metadata:
  name: test-mgmt
platform:
  aws:
    defaultMachinePlatform:
      zones:
      - eu-central-1b
    region: eu-central-1
pullSecret: '{"auths":{}}'
`,
		},
		"workers in all zones with SSH key": {
			params: ocp.InstallConfigParams{
				Name:         "test-mgmt",
				BaseDomain:   "example.com",
				Region:       "eu-central-1",
				InstanceType: "m6i.xlarge",
				Workers:      3,
				PullSecret:   `{"auths":{}}`,
				SSHKey:       "ssh-ed25519 AAAA test@example.com",
			},
			want: `apiVersion: v1
baseDomain: example.com
compute:
- name: worker
  platform:
    aws:
      type: m6i.xlarge
  replicas: 3
controlPlane:
  name: master
  platform:
    aws:
      type: m6i.xlarge
  replicas: 3
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

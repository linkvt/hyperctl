package app_test

import (
	"path/filepath"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestCreateManagementClusterSize(t *testing.T) {
	tests := map[string]struct {
		args   []string
		config string
		want   mgmtSize
	}{
		"compact t3a in the first zone by default": {
			want: mgmtSize{instanceType: "t3a.xlarge", workers: 0, zones: []string{"eu-central-1b"}},
		},
		"workers in all zones": {
			args: []string{"--instance-type", "m6i.xlarge", "--workers", "3"},
			want: mgmtSize{instanceType: "m6i.xlarge", workers: 3},
		},
		"from the config file": {
			config: "mgmtInstanceType: m6i.xlarge\nmgmtWorkers: 3\n",
			want:   mgmtSize{instanceType: "m6i.xlarge", workers: 3},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			if tc.config != "" {
				configPath := filepath.Join(env.home, ".config", "hyperctl", "config.yaml")
				mustMkdir(t, filepath.Dir(configPath))
				writeFile(t, configPath, tc.config, 0o600)
			}

			env.run(t, append([]string{"create",
				"--release", "5.1.0-ec.0",
				"--region", "eu-central-1",
				"--name", "test-mgmt",
				"--pull-secret", env.pullSecret,
			}, tc.args...)...)

			if got := parseMgmtSize(t, env.installerCalls(t)[0].installConfig); !got.equal(tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// mgmtSize is the machine configuration of an install-config.
type mgmtSize struct {
	instanceType string
	workers      int
	zones        []string
}

func (s mgmtSize) equal(o mgmtSize) bool {
	return s.instanceType == o.instanceType && s.workers == o.workers && slices.Equal(s.zones, o.zones)
}

func parseMgmtSize(t *testing.T, installConfig string) mgmtSize {
	t.Helper()
	type pool struct {
		Platform struct {
			AWS struct {
				Type string `json:"type"`
			} `json:"aws"`
		} `json:"platform"`
		Replicas int `json:"replicas"`
	}
	var c struct {
		Compute      []pool `json:"compute"`
		ControlPlane pool   `json:"controlPlane"`
		Platform     struct {
			AWS struct {
				DefaultMachinePlatform struct {
					Zones []string `json:"zones"`
				} `json:"defaultMachinePlatform"`
			} `json:"aws"`
		} `json:"platform"`
	}
	if err := yaml.Unmarshal([]byte(installConfig), &c); err != nil {
		t.Fatalf("parsing install-config: %v", err)
	}
	if c.ControlPlane.Replicas != 3 || len(c.Compute) != 1 || c.Compute[0].Platform.AWS.Type != c.ControlPlane.Platform.AWS.Type {
		t.Fatalf("unexpected machine pools in install-config:\n%s", installConfig)
	}
	return mgmtSize{
		instanceType: c.ControlPlane.Platform.AWS.Type,
		workers:      c.Compute[0].Replicas,
		zones:        c.Platform.AWS.DefaultMachinePlatform.Zones,
	}
}

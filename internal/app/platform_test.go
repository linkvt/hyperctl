package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// awsOptionsView and azureOptionsView are the options of a create run that
// the tests compare.
type awsOptionsView struct {
	region     string
	baseDomain string
	oidcBucket string
	size       openshiftSize
}

type azureOptionsView struct {
	region     string
	baseDomain string
	size       aksSize
}

func TestAWSCreateOptions(t *testing.T) {
	workers := 3
	tests := map[string]struct {
		args        []string
		st          state
		cfg         config
		user        string
		want        awsOptionsView
		wantMissing []string
	}{
		"built-in defaults": {
			user: "tester",
			want: awsOptionsView{region: "us-east-2", baseDomain: defaultBaseDomain, oidcBucket: "tester-hypershift-oidc", size: openshiftSize{"t3a.xlarge", 1}},
		},
		"from the config file": {
			user: "tester",
			cfg:  config{Region: "eu-west-1", BaseDomain: "config.example.com", OIDCBucket: "config-oidc", MgmtInstanceType: "m6i.xlarge", MgmtWorkers: &workers},
			want: awsOptionsView{region: "eu-west-1", baseDomain: "config.example.com", oidcBucket: "config-oidc", size: openshiftSize{"m6i.xlarge", 3}},
		},
		"last used region": {
			user: "tester",
			st:   state{LastRegions: lastRegions{AWS: "eu-north-1", Azure: "eastus"}},
			want: awsOptionsView{region: "eu-north-1", baseDomain: defaultBaseDomain, oidcBucket: "tester-hypershift-oidc", size: openshiftSize{"t3a.xlarge", 1}},
		},
		"flags override the config file": {
			args: []string{"--oidc-bucket", "flag-oidc", "--instance-type", "m6i.large", "--workers", "0"},
			user: "tester",
			cfg:  config{OIDCBucket: "config-oidc", MgmtInstanceType: "m6i.xlarge", MgmtWorkers: &workers},
			want: awsOptionsView{region: "us-east-2", baseDomain: defaultBaseDomain, oidcBucket: "flag-oidc", size: openshiftSize{"m6i.large", 0}},
		},
		"no OIDC bucket without USER": {
			want:        awsOptionsView{region: "us-east-2", baseDomain: defaultBaseDomain, size: openshiftSize{"t3a.xlarge", 1}},
			wantMissing: []string{"--oidc-bucket"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("USER", tc.user)
			aws := awsPlatform{deps: Deps{DefaultRegion: "us-east-2"}}
			run, err := aws.newCreate(parseCreateFlags(t, tc.args...), tc.st, tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			r := run.(*awsCreate)
			got := awsOptionsView{region: r.defaultRegion(), baseDomain: r.defaultBaseDomain(), oidcBucket: r.oidcBucket, size: r.size}
			if got != tc.want {
				t.Errorf("options:\ngot:  %+v\nwant: %+v", got, tc.want)
			}
			if got := r.missing(); !slices.Equal(got, tc.wantMissing) {
				t.Errorf("missing: got %v, want %v", got, tc.wantMissing)
			}
		})
	}
}

func TestAzureCreateOptions(t *testing.T) {
	tests := map[string]struct {
		args    []string
		st      state
		cfg     config
		want    azureOptionsView
		wantErr string
	}{
		"built-in defaults": {
			want: azureOptionsView{region: "westus3", baseDomain: defaultAzureBaseDomain, size: aksSize{"Standard_D4s_v5", 3}},
		},
		"from the config file": {
			cfg:  config{Azure: azureConfig{Location: "eastus", BaseDomain: "azure.example.com", MgmtVMSize: "Standard_D8s_v5", MgmtNodes: 2}},
			want: azureOptionsView{region: "eastus", baseDomain: "azure.example.com", size: aksSize{"Standard_D8s_v5", 2}},
		},
		"last used location": {
			st:   state{LastRegions: lastRegions{AWS: "eu-north-1", Azure: "germanywestcentral"}},
			want: azureOptionsView{region: "germanywestcentral", baseDomain: defaultAzureBaseDomain, size: aksSize{"Standard_D4s_v5", 3}},
		},
		"flags override the config file": {
			args: []string{"--instance-type", "Standard_D2s_v5", "--workers", "5"},
			cfg:  config{Azure: azureConfig{MgmtVMSize: "Standard_D8s_v5", MgmtNodes: 2}},
			want: azureOptionsView{region: "westus3", baseDomain: defaultAzureBaseDomain, size: aksSize{"Standard_D2s_v5", 5}},
		},
		"rejects the OIDC bucket": {
			args:    []string{"--oidc-bucket", "my-oidc"},
			wantErr: "--oidc-bucket does not apply to azure",
		},
		"rejects the embedded karpenter-operator": {
			args:    []string{"--standalone-karpenter-operator=false"},
			wantErr: "requires the standalone karpenter-operator",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			run, err := azurePlatform{}.newCreate(parseCreateFlags(t, tc.args...), tc.st, tc.cfg)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected an error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := run.(*azureCreate)
			got := azureOptionsView{region: r.defaultRegion(), baseDomain: r.defaultBaseDomain(), size: r.size}
			if got != tc.want {
				t.Errorf("options:\ngot:  %+v\nwant: %+v", got, tc.want)
			}
			if got := r.missing(); len(got) != 0 {
				t.Errorf("missing: got %v, want none", got)
			}
		})
	}
}

// parseCreateFlags returns the create command with the flags parsed from
// args.
func parseCreateFlags(t *testing.T, args ...string) *cli.Command {
	t.Helper()
	cmd := createCommand(Deps{})
	var parsed *cli.Command
	cmd.Action = func(_ context.Context, c *cli.Command) error {
		parsed = c
		return nil
	}
	if err := cmd.Run(t.Context(), append([]string{"create"}, args...)); err != nil {
		t.Fatal(err)
	}
	return parsed
}

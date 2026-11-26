package hypershift

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/linkvt/hyperctl/internal/process/processtest"
)

const (
	testPath       = "/bin/hypershift"
	testKubeconfig = "/mgmt/kubeconfig"
)

var testEnv = []string{"KUBECONFIG=" + testKubeconfig}

var testDestroyParams = DestroyClusterParams{
	Name:       "workload",
	InfraID:    "workload-abcde",
	Region:     "eu-central-1",
	BaseDomain: "example.com",
}

var testAzureDestroyParams = DestroyAzureParams{
	Name:                 "workload",
	InfraID:              "workload-abcde",
	CredentialsFile:      "/azure/credentials.json",
	Location:             "eastus",
	DNSZoneResourceGroup: "os4-common",
}

var testAzureClusterArgs = []string{"create", "cluster", "azure",
	"--name", "workload",
	"--infra-id", "workload-abcde",
	"--azure-creds", "/azure/credentials.json",
	"--location", "eastus",
	"--base-domain", "azure.example.com",
	"--dns-zone-rg-name", "os4-common",
	"--pull-secret", "/pull-secret.json",
	"--release-image", "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
	"--node-pool-replicas", "2",
	"--oidc-issuer-url", "https://issuer.example.com",
	"--sa-token-issuer-private-key-path", "/azure/serviceaccount-signer.private",
	"--infra-json", "/hosted/infra-output.yaml",
	"--diagnostics-storage-account-type", "Managed",
	"--external-dns-domain", "hyperctl.example.com",
	"--annotations", "hypershift.openshift.io/pod-security-admission-label-override=baseline",
	"--auto-node",
	"--wait",
	"--timeout", "45m0s",
}

func TestCLICommands(t *testing.T) {
	tests := map[string]struct {
		run  func(context.Context, CLI) error
		want []processtest.Call
	}{
		"install": {
			run: func(ctx context.Context, c CLI) error {
				return c.Install(ctx, InstallParams{
					OIDCBucket:                  "my-oidc",
					OIDCBucketRegion:            "us-east-2",
					AWSCredentialsFile:          "/home/.aws/credentials",
					StandaloneKarpenterOperator: true,
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"install",
				"--oidc-storage-provider-s3-bucket-name", "my-oidc",
				"--oidc-storage-provider-s3-credentials", "/home/.aws/credentials",
				"--oidc-storage-provider-s3-region", "us-east-2",
				"--tech-preview-no-upgrade",
				"--enable-standalone-karpenter-operator",
				"--wait-until-available",
			}}},
		},
		"install with the embedded karpenter-operator": {
			run: func(ctx context.Context, c CLI) error {
				return c.Install(ctx, InstallParams{
					OIDCBucket:         "my-oidc",
					OIDCBucketRegion:   "us-east-2",
					AWSCredentialsFile: "/home/.aws/credentials",
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"install",
				"--oidc-storage-provider-s3-bucket-name", "my-oidc",
				"--oidc-storage-provider-s3-credentials", "/home/.aws/credentials",
				"--oidc-storage-provider-s3-region", "us-east-2",
				"--tech-preview-no-upgrade",
				"--wait-until-available",
			}}},
		},
		"create cluster": {
			run: func(ctx context.Context, c CLI) error {
				return c.CreateCluster(ctx, CreateClusterParams{
					Name:         "workload",
					InfraID:      "workload-abcde",
					Region:       "eu-central-1",
					BaseDomain:   "example.com",
					PullSecret:   "/pull-secret.json",
					ReleaseImage: "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"create", "cluster", "aws",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--region", "eu-central-1",
				"--base-domain", "example.com",
				"--pull-secret", "/pull-secret.json",
				"--release-image", "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
				"--node-pool-replicas", "1",
				"--auto-node",
				"--wait",
				"--timeout", "45m0s",
			}}},
		},
		"create cluster with SSH key": {
			run: func(ctx context.Context, c CLI) error {
				return c.CreateCluster(ctx, CreateClusterParams{
					Name:         "workload",
					InfraID:      "workload-abcde",
					Region:       "eu-central-1",
					BaseDomain:   "example.com",
					PullSecret:   "/pull-secret.json",
					ReleaseImage: "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
					SSHKey:       "/id_ed25519.pub",
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"create", "cluster", "aws",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--region", "eu-central-1",
				"--base-domain", "example.com",
				"--pull-secret", "/pull-secret.json",
				"--release-image", "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
				"--node-pool-replicas", "1",
				"--auto-node",
				"--wait",
				"--timeout", "45m0s",
				"--ssh-key", "/id_ed25519.pub",
			}}},
		},
		"destroy cluster": {
			run: func(ctx context.Context, c CLI) error {
				return c.DestroyCluster(ctx, testDestroyParams)
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"destroy", "cluster", "aws",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--region", "eu-central-1",
				"--base-domain", "example.com",
			}}},
		},
		"destroy infra": {
			run: func(ctx context.Context, c CLI) error {
				return c.DestroyInfra(ctx, testDestroyParams)
			},
			want: []processtest.Call{
				{Path: testPath, Env: testEnv, Args: []string{"destroy", "infra", "aws",
					"--infra-id", "workload-abcde",
					"--name", "workload",
					"--region", "eu-central-1",
					"--base-domain", "example.com",
				}},
				{Path: testPath, Env: testEnv, Args: []string{"destroy", "iam", "aws",
					"--infra-id", "workload-abcde",
					"--region", "eu-central-1",
				}},
			},
		},
		"hosted kubeconfig": {
			run: func(ctx context.Context, c CLI) error {
				_, err := c.HostedKubeconfig(ctx, "workload")
				return err
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"create", "kubeconfig", "--name", "workload"}}},
		},
		"version": {
			run: func(ctx context.Context, c CLI) error {
				return c.Version(ctx)
			},
			want: []processtest.Call{{Path: testPath, Args: []string{"--version"}}},
		},
		"install on Azure": {
			run: func(ctx context.Context, c CLI) error {
				return c.InstallAzure(ctx, InstallAzureParams{
					PullSecret:                 "/pull-secret.json",
					ExternalDNSCredentialsFile: "/azure/external-dns.json",
					ExternalDNSDomain:          "hyperctl.example.com",
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"install",
				"--pull-secret", "/pull-secret.json",
				"--limit-crd-install", "Azure",
				"--tech-preview-no-upgrade",
				"--enable-standalone-karpenter-operator",
				"--external-dns-provider", "azure",
				"--external-dns-credentials", "/azure/external-dns.json",
				"--external-dns-domain-filter", "hyperctl.example.com",
				"--wait-until-available",
			}}},
		},
		"create Azure IAM": {
			run: func(ctx context.Context, c CLI) error {
				return c.CreateAzureIAM(ctx, CreateAzureIAMParams{
					Name:            "workload",
					InfraID:         "workload-abcde",
					CredentialsFile: "/azure/credentials.json",
					Location:        "eastus",
					ResourceGroup:   "tester-hyperctl",
					OIDCIssuerURL:   "https://issuer.example.com",
					OutputFile:      "/hosted/workload-identities.json",
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"create", "iam", "azure",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--azure-creds", "/azure/credentials.json",
				"--location", "eastus",
				"--resource-group-name", "tester-hyperctl",
				"--oidc-issuer-url", "https://issuer.example.com",
				"--output-file", "/hosted/workload-identities.json",
				"--enable-karpenter",
			}}},
		},
		"create Azure infra": {
			run: func(ctx context.Context, c CLI) error {
				return c.CreateAzureInfra(ctx, CreateAzureInfraParams{
					Name:                   "workload",
					InfraID:                "workload-abcde",
					CredentialsFile:        "/azure/credentials.json",
					BaseDomain:             "azure.example.com",
					Location:               "eastus",
					WorkloadIdentitiesFile: "/hosted/workload-identities.json",
					DNSZoneResourceGroup:   "os4-common",
					OutputFile:             "/hosted/infra-output.yaml",
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"create", "infra", "azure",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--azure-creds", "/azure/credentials.json",
				"--base-domain", "azure.example.com",
				"--location", "eastus",
				"--workload-identities-file", "/hosted/workload-identities.json",
				"--dns-zone-rg-name", "os4-common",
				"--output-file", "/hosted/infra-output.yaml",
			}}},
		},
		"create Azure cluster": {
			run: func(ctx context.Context, c CLI) error {
				return c.CreateAzureCluster(ctx, testAzureClusterParams(""))
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: slices.Concat(testAzureClusterArgs, []string{"--generate-ssh"})}},
		},
		"create Azure cluster with SSH key": {
			run: func(ctx context.Context, c CLI) error {
				return c.CreateAzureCluster(ctx, testAzureClusterParams("/id_ed25519.pub"))
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: slices.Concat(testAzureClusterArgs, []string{"--ssh-key", "/id_ed25519.pub"})}},
		},
		"destroy Azure cluster": {
			run: func(ctx context.Context, c CLI) error {
				return c.DestroyAzureCluster(ctx, testAzureDestroyParams)
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"destroy", "cluster", "azure",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--azure-creds", "/azure/credentials.json",
				"--location", "eastus",
				"--dns-zone-rg-name", "os4-common",
			}}},
		},
		"destroy Azure infra": {
			run: func(ctx context.Context, c CLI) error {
				return c.DestroyAzureInfra(ctx, testAzureDestroyParams)
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"destroy", "infra", "azure",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--azure-creds", "/azure/credentials.json",
				"--location", "eastus",
			}}},
		},
		"destroy Azure IAM": {
			run: func(ctx context.Context, c CLI) error {
				return c.DestroyAzureIAM(ctx, DestroyAzureIAMParams{
					Name:                   "workload",
					InfraID:                "workload-abcde",
					CredentialsFile:        "/azure/credentials.json",
					ResourceGroup:          "hyperctl-shared",
					WorkloadIdentitiesFile: "/hosted/workload-identities.json",
					DNSZoneResourceGroup:   "os4-common",
				})
			},
			want: []processtest.Call{{Path: testPath, Env: testEnv, Args: []string{"destroy", "iam", "azure",
				"--name", "workload",
				"--infra-id", "workload-abcde",
				"--azure-creds", "/azure/credentials.json",
				"--resource-group-name", "hyperctl-shared",
				"--workload-identities-file", "/hosted/workload-identities.json",
				"--dns-zone-rg-name", "os4-common",
			}}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := &processtest.Recorder{}
			cli := CLI{path: testPath, kubeconfig: testKubeconfig, output: &bytes.Buffer{}, runner: recorder}

			if err := tc.run(context.Background(), cli); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(recorder.Calls, tc.want) {
				t.Errorf("calls:\ngot:  %q\nwant: %q", recorder.Calls, tc.want)
			}
		})
	}
}

func TestCLIStreamsOutput(t *testing.T) {
	recorder := &processtest.Recorder{Results: []processtest.Result{{Stdout: "installing\n", Stderr: "warning\n"}}}
	var output bytes.Buffer
	cli := CLI{path: testPath, kubeconfig: testKubeconfig, output: &output, runner: recorder}

	if err := cli.Install(context.Background(), InstallParams{}); err != nil {
		t.Fatal(err)
	}

	if got, want := output.String(), "installing\nwarning\n"; got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

func TestCLIReportsFailedSubcommand(t *testing.T) {
	recorder := &processtest.Recorder{Results: []processtest.Result{{Err: errors.New("exit status 1")}}}
	cli := CLI{path: testPath, kubeconfig: testKubeconfig, output: &bytes.Buffer{}, runner: recorder}

	err := cli.DestroyCluster(context.Background(), testDestroyParams)

	if want := "hypershift destroy cluster: exit status 1"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

func TestCLIDestroyInfraStopsAfterFailure(t *testing.T) {
	recorder := &processtest.Recorder{Results: []processtest.Result{{Err: errors.New("exit status 1")}}}
	cli := CLI{path: testPath, kubeconfig: testKubeconfig, output: &bytes.Buffer{}, runner: recorder}

	if err := cli.DestroyInfra(context.Background(), testDestroyParams); err == nil {
		t.Fatal("expected an error")
	}

	if len(recorder.Calls) != 1 {
		t.Errorf("expected no IAM destroy after a failed infra destroy, got %q", recorder.Calls)
	}
}

func TestCLIHostedKubeconfig(t *testing.T) {
	recorder := &processtest.Recorder{Results: []processtest.Result{{Stdout: "kind: Config\n", Stderr: "fetching\n"}}}
	var output bytes.Buffer
	cli := CLI{path: testPath, kubeconfig: testKubeconfig, output: &output, runner: recorder}

	kubeconfig, err := cli.HostedKubeconfig(context.Background(), "workload")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(kubeconfig), "kind: Config\n"; got != want {
		t.Errorf("kubeconfig: got %q, want %q", got, want)
	}
	if got, want := output.String(), "fetching\n"; got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

func TestCLIVersionReportsOutput(t *testing.T) {
	recorder := &processtest.Recorder{Results: []processtest.Result{{Stderr: "exec format error", Err: errors.New("exit status 126")}}}
	cli := CLI{path: testPath, runner: recorder}

	err := cli.Version(context.Background())

	for _, want := range []string{"running /bin/hypershift --version: exit status 126", "exec format error"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("expected error containing %q, got %v", want, err)
		}
	}
}

func testAzureClusterParams(sshKey string) CreateAzureClusterParams {
	return CreateAzureClusterParams{
		Name:                 "workload",
		InfraID:              "workload-abcde",
		CredentialsFile:      "/azure/credentials.json",
		Location:             "eastus",
		BaseDomain:           "azure.example.com",
		DNSZoneResourceGroup: "os4-common",
		PullSecret:           "/pull-secret.json",
		ReleaseImage:         "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
		SSHKey:               sshKey,
		OIDCIssuerURL:        "https://issuer.example.com",
		SigningKeyFile:       "/azure/serviceaccount-signer.private",
		InfraFile:            "/hosted/infra-output.yaml",
		ExternalDNSDomain:    "hyperctl.example.com",
	}
}

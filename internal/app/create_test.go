package app_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
)

func TestCreateRunsInstallerWithReleaseAndInstallConfig(t *testing.T) {
	env := newTestEnv(t)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	calls := env.images.creates
	if len(calls) != 1 {
		t.Fatalf("expected 1 installer call, got %d", len(calls))
	}
	call := calls[0]
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install"); call.installDir != want {
		t.Errorf("install dir: got %q, want %q", call.installDir, want)
	}
	if want := "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64"; call.release.PullSpec != want {
		t.Errorf("release: got %q, want %q", call.release.PullSpec, want)
	}

	var installConfig struct {
		BaseDomain string `json:"baseDomain"`
		Metadata   struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Platform struct {
			AWS struct {
				Region string `json:"region"`
			} `json:"aws"`
		} `json:"platform"`
		PullSecret string `json:"pullSecret"`
	}
	if err := yaml.Unmarshal([]byte(call.installConfig), &installConfig); err != nil {
		t.Fatalf("parsing install-config: %v", err)
	}
	if installConfig.Metadata.Name != "test-mgmt" {
		t.Errorf("metadata.name: got %q", installConfig.Metadata.Name)
	}
	if installConfig.BaseDomain != "example.com" {
		t.Errorf("baseDomain: got %q", installConfig.BaseDomain)
	}
	if installConfig.Platform.AWS.Region != "eu-central-1" {
		t.Errorf("platform.aws.region: got %q", installConfig.Platform.AWS.Region)
	}
	if installConfig.PullSecret != testPullSecret {
		t.Errorf("pullSecret: got %q", installConfig.PullSecret)
	}
}

func TestCreateRemembersRegion(t *testing.T) {
	env := newTestEnv(t)
	env.defaultRegion = "us-east-2"
	env.run(t, "create", "--release", "5.1.0-ec.0", "--region", "eu-west-1", "--name", "first", "--pull-secret", env.pullSecret)
	env.run(t, "destroy", "--yes")

	env.run(t, "create", "--release", "5.1.0-ec.0", "--name", "second", "--pull-secret", env.pullSecret)

	var installConfig struct {
		Platform struct {
			AWS struct {
				Region string `json:"region"`
			} `json:"aws"`
		} `json:"platform"`
	}
	if err := yaml.Unmarshal([]byte(env.images.creates[1].installConfig), &installConfig); err != nil {
		t.Fatalf("parsing install-config: %v", err)
	}
	if got, want := installConfig.Platform.AWS.Region, "eu-west-1"; got != want {
		t.Errorf("region of the second cluster: got %q, want %q", got, want)
	}
}

func TestCreateUsesDefaultsForMissingValues(t *testing.T) {
	env := newTestEnv(t)
	env.defaultRegion = "us-east-2"
	defaultPullSecret := filepath.Join(env.home, ".openshift", "pull-secret-latest.json")
	mustMkdir(t, filepath.Dir(defaultPullSecret))
	writeFile(t, defaultPullSecret, testPullSecret, 0o600)

	env.run(t, "create", "--release", "5.1.0-ec.0")

	var installConfig struct {
		BaseDomain string `json:"baseDomain"`
		Metadata   struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Platform struct {
			AWS struct {
				Region string `json:"region"`
			} `json:"aws"`
		} `json:"platform"`
		PullSecret string `json:"pullSecret"`
	}
	if err := yaml.Unmarshal([]byte(env.images.creates[0].installConfig), &installConfig); err != nil {
		t.Fatalf("parsing install-config: %v", err)
	}
	if installConfig.Metadata.Name != "tester-karpenter" {
		t.Errorf("metadata.name: got %q", installConfig.Metadata.Name)
	}
	if installConfig.BaseDomain != "autoscaling-dev.devcluster.openshift.com" {
		t.Errorf("baseDomain: got %q", installConfig.BaseDomain)
	}
	if installConfig.Platform.AWS.Region != "us-east-2" {
		t.Errorf("platform.aws.region: got %q", installConfig.Platform.AWS.Region)
	}
	if installConfig.PullSecret != testPullSecret {
		t.Errorf("pullSecret: got %q", installConfig.PullSecret)
	}
}

func TestCreateUsesConfigFileDefaults(t *testing.T) {
	env := newTestEnv(t)
	env.defaultRegion = "us-east-2"
	sshKey := filepath.Join(env.home, "id_ed25519.pub")
	writeFile(t, sshKey, "ssh-ed25519 AAAA test@example.com\n", 0o644)
	configPath := filepath.Join(env.home, ".config", "hyperctl", "config.yaml")
	mustMkdir(t, filepath.Dir(configPath))
	writeFile(t, configPath, `region: eu-west-1
baseDomain: config.example.com
pullSecret: `+env.pullSecret+`
sshKey: `+sshKey+`
`, 0o600)

	env.run(t, "create", "--release", "5.1.0-ec.0", "--name", "test-mgmt")

	var installConfig struct {
		BaseDomain string `json:"baseDomain"`
		Platform   struct {
			AWS struct {
				Region string `json:"region"`
			} `json:"aws"`
		} `json:"platform"`
		PullSecret string `json:"pullSecret"`
		SSHKey     string `json:"sshKey"`
	}
	if err := yaml.Unmarshal([]byte(env.images.creates[0].installConfig), &installConfig); err != nil {
		t.Fatalf("parsing install-config: %v", err)
	}
	if installConfig.Platform.AWS.Region != "eu-west-1" {
		t.Errorf("platform.aws.region: got %q", installConfig.Platform.AWS.Region)
	}
	if installConfig.BaseDomain != "config.example.com" {
		t.Errorf("baseDomain: got %q", installConfig.BaseDomain)
	}
	if installConfig.PullSecret != testPullSecret {
		t.Errorf("pullSecret: got %q", installConfig.PullSecret)
	}
	if installConfig.SSHKey != "ssh-ed25519 AAAA test@example.com" {
		t.Errorf("sshKey: got %q", installConfig.SSHKey)
	}
}

func TestCreateExpandsHomeInConfigPaths(t *testing.T) {
	env := newTestEnv(t)
	mustMkdir(t, filepath.Join(env.home, ".secrets"))
	writeFile(t, filepath.Join(env.home, ".secrets", "pull-secret.json"), testPullSecret, 0o600)
	configPath := filepath.Join(env.home, ".config", "hyperctl", "config.yaml")
	mustMkdir(t, filepath.Dir(configPath))
	writeFile(t, configPath, "pullSecret: ~/.secrets/pull-secret.json\n", 0o600)

	env.run(t, "create", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt")

	var installConfig struct {
		PullSecret string `json:"pullSecret"`
	}
	if err := yaml.Unmarshal([]byte(env.images.creates[0].installConfig), &installConfig); err != nil {
		t.Fatalf("parsing install-config: %v", err)
	}
	if installConfig.PullSecret != testPullSecret {
		t.Errorf("pullSecret: got %q", installConfig.PullSecret)
	}
}

func TestCreateDefaultsToLastUsedRelease(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "first",
		"--pull-secret", env.pullSecret,
	)
	env.run(t, "destroy", "--yes")

	env.run(t, "create",
		"--region", "eu-central-1",
		"--name", "second",
		"--pull-secret", env.pullSecret,
	)

	calls := env.images.creates
	if want := "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64"; calls[1].release.PullSpec != want {
		t.Errorf("release: got %q, want %q", calls[1].release.PullSpec, want)
	}
}

// TestCreateWithYesDoesNotPrompt runs create in a terminal with --yes and
// expects the defaults to be used instead of prompts.
func TestCreateWithYesDoesNotPrompt(t *testing.T) {
	env := newTestEnv(t)
	env.interactive = true

	env.run(t, "create", "--yes", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--pull-secret", env.pullSecret)

	if len(env.images.creates) != 1 {
		t.Fatalf("expected the management cluster to be created, got %d installations", len(env.images.creates))
	}
	if got := len(env.hypershift.clusters); got != 1 {
		t.Errorf("expected the hosted cluster to be created, got %d", got)
	}
}

// TestCreateWithYesRequiresHypershift expects --yes to fail instead of
// asking for the path of the hypershift binary in a terminal.
func TestCreateWithYesRequiresHypershift(t *testing.T) {
	env := newTestEnv(t)
	env.interactive = true
	env.hypershift.path = ""

	_, err := env.runErr(t, "create", "--yes", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "hypershift binary not found") {
		t.Errorf("expected an error about the missing hypershift binary, got %v", err)
	}
}
func TestCreateRequiresReleaseWhenNotInteractive(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "create", "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "--release") {
		t.Errorf("expected missing --release error, got %v", err)
	}
}

func TestCreateAcceptsReleasePullSpec(t *testing.T) {
	env := newTestEnv(t)
	pullSpec := "registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-25-074941"

	env.run(t, "create",
		"--release", pullSpec,
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if got := env.images.creates[0].release.PullSpec; got != pullSpec {
		t.Errorf("release: got %q, want %q", got, pullSpec)
	}
	if got := single(t, env.hypershift.clusters).params.ReleaseImage; got != pullSpec {
		t.Errorf("hosted cluster release image: got %q, want %q", got, pullSpec)
	}
}

func TestCreateAddsSSHKeyToInstallConfig(t *testing.T) {
	env := newTestEnv(t)
	sshKey := filepath.Join(env.home, "id_ed25519.pub")
	writeFile(t, sshKey, "ssh-ed25519 AAAA test@example.com\n", 0o644)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--ssh-key", sshKey,
	)

	var installConfig struct {
		SSHKey string `json:"sshKey"`
	}
	if err := yaml.Unmarshal([]byte(env.images.creates[0].installConfig), &installConfig); err != nil {
		t.Fatalf("parsing install-config: %v", err)
	}
	if want := "ssh-ed25519 AAAA test@example.com"; installConfig.SSHKey != want {
		t.Errorf("sshKey: got %q, want %q", installConfig.SSHKey, want)
	}
}

func TestCreateReusesCachedInstaller(t *testing.T) {
	env := newTestEnv(t)

	for _, name := range []string{"first", "second"} {
		env.run(t, "create",
			"--release", "5.1.0-ec.0",
			"--region", "eu-central-1",
			"--base-domain", "example.com",
			"--name", name,
			"--pull-secret", env.pullSecret,
		)
		env.run(t, "destroy", "--yes")
	}

	if got := env.images.extractions; got != 1 {
		t.Errorf("expected 1 installer extraction, got %d", got)
	}
	if got := len(env.images.creates); got != 2 {
		t.Errorf("expected 2 installer calls, got %d", got)
	}
}

func TestCreateKeepsInstallConfigCopy(t *testing.T) {
	env := newTestEnv(t)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	kept, err := os.ReadFile(filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if consumed := env.images.creates[0].installConfig; string(kept) != consumed {
		t.Errorf("kept install-config differs from the consumed one:\nkept:\n%s\nconsumed:\n%s", kept, consumed)
	}
}

func TestCreateRefusesAssumedRoleCredentials(t *testing.T) {
	env := newTestEnv(t)
	env.aws.callerARN = "arn:aws:sts::123456789012:assumed-role/123456789012-admin/tester"

	_, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if err == nil || !strings.Contains(err.Error(), "assumed role") {
		t.Errorf("expected assumed role error, got %v", err)
	}
	if got := env.images.extractions + len(env.images.creates); got != 0 {
		t.Errorf("expected no oc or installer calls, got %d", got)
	}
}

func TestCreateRequiresSpotServiceLinkedRole(t *testing.T) {
	env := newTestEnv(t)
	env.aws.spotRoleMissing = true

	_, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if err == nil || !strings.Contains(err.Error(), "AWSServiceRoleForEC2Spot") {
		t.Errorf("expected missing Spot role error, got %v", err)
	}
	if got := len(env.images.creates); got != 0 {
		t.Errorf("expected no installer calls, got %d", got)
	}
}

func TestCreateCreatesMissingOIDCBucket(t *testing.T) {
	env := newTestEnv(t)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if got := env.aws.publicBuckets["tester-hypershift-oidc"]; got != "eu-central-1" {
		t.Errorf("expected public bucket tester-hypershift-oidc in eu-central-1, got buckets %v", env.aws.publicBuckets)
	}
}

func TestCreateReusesExistingOIDCBucket(t *testing.T) {
	env := newTestEnv(t)
	env.aws.publicBuckets["my-oidc"] = "us-east-2"

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--oidc-bucket", "my-oidc",
	)

	if env.aws.bucketsCreated != 0 {
		t.Errorf("expected no bucket to be created, got %d", env.aws.bucketsCreated)
	}
}

func TestCreateInstallsHyperShiftOperator(t *testing.T) {
	env := newTestEnv(t)
	env.aws.publicBuckets["my-oidc"] = "us-east-2"

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--oidc-bucket", "my-oidc",
	)

	call := single(t, env.hypershift.installs)
	want := hypershift.InstallParams{
		OIDCBucket:                  "my-oidc",
		OIDCBucketRegion:            "us-east-2",
		AWSCredentialsFile:          filepath.Join(env.home, ".aws", "credentials"),
		StandaloneKarpenterOperator: true,
	}
	if call.params != want {
		t.Errorf("hypershift install:\ngot:  %+v\nwant: %+v", call.params, want)
	}
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig"); call.kubeconfig != want {
		t.Errorf("hypershift KUBECONFIG: got %q, want %q", call.kubeconfig, want)
	}
}

func TestCreateEmbeddedKarpenterOperator(t *testing.T) {
	env := newTestEnv(t)
	env.aws.publicBuckets["my-oidc"] = "us-east-2"

	env.run(t, "create",
		"--standalone-karpenter-operator=false",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--oidc-bucket", "my-oidc",
	)

	if params := single(t, env.hypershift.installs).params; params.StandaloneKarpenterOperator {
		t.Errorf("expected no standalone Karpenter operator, got hypershift install %+v", params)
	}
}

func TestCreateSetupManagementSkipsHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.aws.dns["test-mgmt-workload"] = []string{"api.test-mgmt-workload.example.com"}

	output := env.run(t, "create",
		"--setup", "management",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	installs := len(env.hypershift.installs)
	hostedCreates := len(env.hypershift.clusters)
	if installs != 1 || hostedCreates != 0 {
		t.Errorf("expected one operator install and no hosted cluster creation, got %d installs and %d hosted creates", installs, hostedCreates)
	}
	if got := env.aws.dns["test-mgmt-workload"]; len(got) != 1 {
		t.Errorf("hosted cluster DNS leftovers should remain untouched, got %v", got)
	}

	if got := env.currentCluster(t); got != "test-mgmt" {
		t.Errorf("current cluster: got %q, want test-mgmt", got)
	}
	if !strings.Contains(output, "Done, use the cluster kubeconfig with:\n") || !strings.Contains(output, "export KUBECONFIG=$(hyperctl kubeconfig)") {
		t.Errorf("setup management completion should use the common kubeconfig command, got:\n%s", output)
	}

	paths := strings.Split(strings.TrimSpace(env.run(t, "kubeconfig")), string(os.PathListSeparator))
	if len(paths) != 1 || paths[0] != filepath.Join(env.home, ".local", "share", "hyperctl", "clusters", "test-mgmt", "install", "auth", "kubeconfig") {
		t.Errorf("setup management kubeconfig paths: got %v", paths)
	}
}

func TestCreateRemembersSetup(t *testing.T) {
	env := newTestEnv(t)
	args := []string{
		"create",
		"--setup", "management",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)
	args = []string{
		"create",
		"--region", "eu-central-1",
		"--name", "second",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)

	for _, call := range env.hypershift.clusters {
		t.Errorf("remembered setup management created a hosted cluster: %+v", call.params)
	}

	var state struct {
		Setup string `json:"setup"`
	}
	statePath := filepath.Join(env.home, ".local", "state", "hyperctl", "state.yaml")
	if err := yaml.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatalf("parsing state: %v", err)
	}
	if state.Setup != "management" {
		t.Errorf("state setup: got %q, want management", state.Setup)
	}
}

func TestCreateCanOverrideRememberedSetup(t *testing.T) {
	env := newTestEnv(t)
	args := []string{
		"create",
		"--setup", "management",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)
	args = []string{
		"create",
		"--setup", "hosted",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "second",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)

	if hostedCreates := len(env.hypershift.clusters); hostedCreates != 1 {
		t.Errorf("expected explicit --setup hosted to create one hosted cluster, got %d", hostedCreates)
	}
}

func TestCreateRejectsManagementOnlyFlag(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "create", "--management-only")

	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -management-only") {
		t.Errorf("expected --management-only to be an unknown flag, got %v", err)
	}
}

// TestCreateSetupStandaloneSkipsHosting expects setup standalone on AWS to install
// only the OpenShift cluster, without the hypershift binary and the account
// prerequisites of the HyperShift operator and hosted clusters.
func TestCreateSetupStandaloneSkipsHosting(t *testing.T) {
	env := newTestEnv(t)
	env.hypershift.path = ""
	env.aws.spotRoleMissing = true
	env.aws.dns["test-mgmt"] = []string{"api.test-mgmt.example.com"}
	if err := os.Remove(filepath.Join(env.home, ".aws", "credentials")); err != nil {
		t.Fatal(err)
	}

	env.run(t, "create", "--setup", "standalone", "--yes",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected openshift-install to create the cluster, got %d installations", got)
	}
	if got := len(env.images.pullChecks); got != 1 {
		t.Errorf("expected the release pull check, got %d", got)
	}
	if got := env.aws.dns["test-mgmt"]; len(got) != 0 {
		t.Errorf("expected the DNS leftovers of the cluster to be deleted, got %v", got)
	}
	if env.aws.bucketsCreated != 0 {
		t.Errorf("expected no OIDC bucket, got %d created", env.aws.bucketsCreated)
	}
	if got := len(env.hypershift.installs); got != 0 {
		t.Errorf("expected no HyperShift operator, got %d installs", got)
	}
	record := readFile(t, filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/cluster.yaml"))
	if !strings.Contains(record, "hypershiftOperator: false") {
		t.Errorf("expected the record to note no HyperShift operator, got:\n%s", record)
	}
}

func TestCreateRejectsFlagsOfOtherSetups(t *testing.T) {
	tests := map[string]struct {
		args    []string
		wantErr string
	}{
		"hosted name with setup management": {
			args:    []string{"--setup", "management", "--hosted-name", "other"},
			wantErr: "--hosted-name does not apply to setup management",
		},
		"hosted name with setup standalone": {
			args:    []string{"--setup", "standalone", "--hosted-name", "other"},
			wantErr: "--hosted-name does not apply to setup standalone",
		},
		"hypershift with setup standalone": {
			args:    []string{"--setup", "standalone", "--hypershift", testHypershiftPath},
			wantErr: "--hypershift does not apply to setup standalone",
		},
		"embedded karpenter-operator with setup standalone": {
			args:    []string{"--setup", "standalone", "--standalone-karpenter-operator=false"},
			wantErr: "--standalone-karpenter-operator=false does not apply to setup standalone",
		},
		"unknown setup": {
			args:    []string{"--setup", "hosted-cluster"},
			wantErr: `unknown setup "hosted-cluster"`,
		},
		"release with setup standalone on azure": {
			args:    []string{"--platform", "azure", "--setup", "standalone"},
			wantErr: "--release does not apply to setup standalone on azure",
		},
		"OIDC bucket with setup standalone": {
			args:    []string{"--setup", "standalone", "--oidc-bucket", "other"},
			wantErr: "--oidc-bucket does not apply to setup standalone",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			args := append([]string{"create",
				"--release", "5.1.0-ec.0",
				"--region", "eu-central-1",
				"--name", "test-mgmt",
				"--pull-secret", env.pullSecret,
			}, tc.args...)

			_, err := env.runErr(t, args...)

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error %q, got %v", tc.wantErr, err)
			}
			if len(env.images.pullChecks) != 0 || len(env.images.creates) != 0 {
				t.Errorf("expected no pull check or installation, got %d pull checks and %d installations", len(env.images.pullChecks), len(env.images.creates))
			}
		})
	}
}

func TestCreateCreatesHostedClusterWithAutoNode(t *testing.T) {
	env := newTestEnv(t)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	call := single(t, env.hypershift.clusters)
	params := call.params
	if !regexp.MustCompile(`^test-mgmt-workload-[a-z0-9]{5}$`).MatchString(params.InfraID) {
		t.Errorf("infra ID: got %q", params.InfraID)
	}
	params.InfraID = ""
	want := hypershift.CreateClusterParams{
		Name:         "test-mgmt-workload",
		Region:       "eu-central-1",
		BaseDomain:   "example.com",
		PullSecret:   env.pullSecret,
		ReleaseImage: "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
	}
	if params != want {
		t.Errorf("hypershift create cluster:\ngot:  %+v\nwant: %+v", params, want)
	}
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig"); call.kubeconfig != want {
		t.Errorf("hypershift KUBECONFIG: got %q, want %q", call.kubeconfig, want)
	}
}

func TestCreateRequiresHypershiftBinary(t *testing.T) {
	env := newTestEnv(t)
	env.hypershift.path = ""

	_, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if err == nil || !strings.Contains(err.Error(), "hypershift") {
		t.Errorf("expected missing hypershift error, got %v", err)
	}
	if got := len(env.images.creates); got != 0 {
		t.Errorf("expected no installer calls, got %d", got)
	}
}

func TestCreateUsesHypershiftFromConfig(t *testing.T) {
	env := newTestEnv(t)
	const path = "/opt/hypershift/bin/hypershift"
	env.hypershift.path = ""
	env.hypershift.binaries = append(env.hypershift.binaries, path)
	configPath := filepath.Join(env.home, ".config", "hyperctl", "config.yaml")
	mustMkdir(t, filepath.Dir(configPath))
	writeFile(t, configPath, "hypershift: "+path+"\n", 0o600)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if got := single(t, env.hypershift.clusters).path; got != path {
		t.Errorf("hypershift binary: got %s, want %s", got, path)
	}
}

func TestCreateSkipsInstalledCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "create")

	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected 1 installer call, got %d", got)
	}
}

func TestCreateRefusesIncompleteInstallation(t *testing.T) {
	env := newTestEnv(t)
	args := []string{"create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	}
	env.images.installErr = errors.New("openshift-install create cluster: exit status 1")
	if _, err := env.runErr(t, args...); err == nil {
		t.Fatal("expected failing installer to fail create")
	}
	env.images.installErr = nil

	_, err := env.runErr(t, args...)

	if err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Errorf("expected incomplete installation error, got %v", err)
	}
	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected 1 installer call, got %d", got)
	}
}

func TestCreateSecondManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "other-mgmt",
		"--pull-secret", env.pullSecret,
	)

	var installDirs []string
	for _, call := range env.images.creates {
		installDirs = append(installDirs, filepath.Base(filepath.Dir(call.installDir)))
	}
	if want := []string{"test-mgmt", "other-mgmt"}; !slices.Equal(installDirs, want) {
		t.Errorf("installed clusters: got %v, want %v", installDirs, want)
	}
	if got := env.hypershift.clusters[1].params.Name; got != "other-mgmt-workload" {
		t.Errorf("hosted cluster of other-mgmt: got %s, want other-mgmt-workload", got)
	}
	if got := env.currentCluster(t); got != "other-mgmt" {
		t.Errorf("current cluster: got %q, want other-mgmt", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestCreateFailsEarlyWhenReleaseImageIsNotPullable(t *testing.T) {
	env := newTestEnv(t)
	env.images.pullErr = errors.New("the release image cannot be pulled:\nerror: unable to read image: unauthorized: authentication required")

	_, err := env.runErr(t, "create",
		"--release", "registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-25-074941",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	for _, want := range []string{"unauthorized", "oc registry login --registry registry.ci.openshift.org --to " + env.pullSecret} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("expected error containing %q, got %v", want, err)
		}
	}
	if calls := env.images.creates; len(calls) != 0 {
		t.Errorf("expected no installer calls, got %d", len(calls))
	}
}

func TestCreateChecksReleaseImageWithPullSecret(t *testing.T) {
	env := newTestEnv(t)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	want := pullCheck{release: ocp.ParseRelease("5.1.0-ec.0"), pullSecret: env.pullSecret}
	if checks := env.images.pullChecks; len(checks) != 1 || checks[0] != want {
		t.Errorf("pull checks: got %+v, want %+v", checks, want)
	}
}

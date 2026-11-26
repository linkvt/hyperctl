package app_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/linkvt/hyperctl/internal/hypershift"
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

	calls := env.installerCalls(t)
	if len(calls) != 1 {
		t.Fatalf("expected 1 installer call, got %d", len(calls))
	}
	call := calls[0]
	if !strings.HasPrefix(call.args, "create cluster --dir ") {
		t.Errorf("unexpected installer args: %q", call.args)
	}
	if want := "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64"; call.releaseOverride != want {
		t.Errorf("release override: got %q, want %q", call.releaseOverride, want)
	}
	if call.disableImagePolicy != "true" {
		t.Errorf("image policy disable: got %q, want %q", call.disableImagePolicy, "true")
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
	if err := yaml.Unmarshal([]byte(env.installerCalls(t)[0].installConfig), &installConfig); err != nil {
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
	if err := yaml.Unmarshal([]byte(env.installerCalls(t)[0].installConfig), &installConfig); err != nil {
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
	if err := yaml.Unmarshal([]byte(env.installerCalls(t)[0].installConfig), &installConfig); err != nil {
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

	calls := env.installerCalls(t)
	if want := "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64"; calls[1].releaseOverride != want {
		t.Errorf("release override: got %q, want %q", calls[1].releaseOverride, want)
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

	if got := env.installerCalls(t)[0].releaseOverride; got != pullSpec {
		t.Errorf("release override: got %q, want %q", got, pullSpec)
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
	if err := yaml.Unmarshal([]byte(env.installerCalls(t)[0].installConfig), &installConfig); err != nil {
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

	if got := env.ocCallCount(t); got != 1 {
		t.Errorf("expected 1 installer extraction, got %d", got)
	}
	if got := len(env.installerCalls(t)); got != 2 {
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

	kept := readFile(t, filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install-config.yaml"))
	if consumed := env.installerCalls(t)[0].installConfig; kept != consumed {
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
	if got := env.ocCallCount(t) + len(env.installerCalls(t)); got != 0 {
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
	if got := len(env.installerCalls(t)); got != 0 {
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

func TestCreateManagementOnlySkipsHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.aws.dns["test-mgmt-workload"] = []string{"api.test-mgmt-workload.example.com"}

	output := env.run(t, "create",
		"--management-only",
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

	var state struct {
		MgmtCluster   string `json:"mgmtCluster"`
		HostedCluster string `json:"hostedCluster"`
	}
	statePath := filepath.Join(env.home, ".local", "state", "hyperctl", "state.yaml")
	if err := yaml.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatalf("parsing state: %v", err)
	}
	if state.MgmtCluster != "test-mgmt" || state.HostedCluster != "" {
		t.Errorf("state: got management %q, hosted %q; want management test-mgmt and no hosted cluster", state.MgmtCluster, state.HostedCluster)
	}
	if !strings.Contains(output, "Done, use the cluster kubeconfig with:\n") || !strings.Contains(output, "export KUBECONFIG=$(hyperctl kubeconfig)") {
		t.Errorf("management-only completion should use the common kubeconfig command, got:\n%s", output)
	}

	paths := strings.Split(strings.TrimSpace(env.run(t, "kubeconfig")), string(os.PathListSeparator))
	if len(paths) != 1 || paths[0] != filepath.Join(env.home, ".local", "share", "hyperctl", "clusters", "test-mgmt", "install", "auth", "kubeconfig") {
		t.Errorf("management-only kubeconfig paths: got %v", paths)
	}
}

func TestCreateRemembersManagementOnlyMode(t *testing.T) {
	env := newTestEnv(t)
	args := []string{
		"create",
		"--management-only",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)
	args = []string{
		"create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)

	for _, call := range env.hypershift.clusters {
		t.Errorf("remembered management-only mode created a hosted cluster: %+v", call.params)
	}

	var state struct {
		ManagementOnly bool `json:"managementOnly"`
	}
	statePath := filepath.Join(env.home, ".local", "state", "hyperctl", "state.yaml")
	if err := yaml.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatalf("parsing state: %v", err)
	}
	if !state.ManagementOnly {
		t.Error("state did not remember management-only mode")
	}
}

func TestCreateCanOverrideRememberedManagementOnlyMode(t *testing.T) {
	env := newTestEnv(t)
	args := []string{
		"create",
		"--management-only",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)
	args = []string{
		"create",
		"--management-only=false",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	}
	env.run(t, args...)

	if hostedCreates := len(env.hypershift.clusters); hostedCreates != 1 {
		t.Errorf("expected explicit management-only=false to create one hosted cluster, got %d", hostedCreates)
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
	if got := len(env.installerCalls(t)); got != 0 {
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

	for range 2 {
		env.run(t, "create",
			"--release", "5.1.0-ec.0",
			"--region", "eu-central-1",
			"--base-domain", "example.com",
			"--name", "test-mgmt",
			"--pull-secret", env.pullSecret,
		)
	}

	if got := len(env.installerCalls(t)); got != 1 {
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
	t.Setenv("HYPERCTL_TEST_INSTALLER_FAIL", "1")
	if _, err := env.runErr(t, args...); err == nil {
		t.Fatal("expected failing installer to fail create")
	}
	t.Setenv("HYPERCTL_TEST_INSTALLER_FAIL", "")

	_, err := env.runErr(t, args...)

	if err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Errorf("expected incomplete installation error, got %v", err)
	}
	if got := len(env.installerCalls(t)); got != 1 {
		t.Errorf("expected 1 installer call, got %d", got)
	}
}

func TestCreateSkipsCreatedHostedCluster(t *testing.T) {
	env := newTestEnv(t)

	for range 2 {
		env.run(t, "create",
			"--release", "5.1.0-ec.0",
			"--region", "eu-central-1",
			"--name", "test-mgmt",
			"--pull-secret", env.pullSecret,
		)
	}

	if creates := len(env.hypershift.clusters); creates != 1 {
		t.Errorf("expected 1 hosted cluster creation, got %d", creates)
	}
}

func TestCreateRefusesSecondManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	_, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "other-mgmt",
		"--pull-secret", env.pullSecret,
	)

	if err == nil || !strings.Contains(err.Error(), "test-mgmt") {
		t.Errorf("expected error about existing management cluster test-mgmt, got %v", err)
	}
	if got := len(env.installerCalls(t)); got != 1 {
		t.Errorf("expected 1 installer call, got %d", got)
	}
}

// installerCall is one recorded invocation of the fake openshift-install.
type installerCall struct {
	args               string
	releaseOverride    string
	disableImagePolicy string
	installConfig      string
}

// installerCalls reads the "create cluster" invocations recorded by the fake
// openshift-install.
func (e *testEnv) installerCalls(t *testing.T) []installerCall {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(e.logDir, "openshift-install-*"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []installerCall
	for _, dir := range dirs {
		args := readFile(t, filepath.Join(dir, "args"))
		if !strings.HasPrefix(args, "create ") {
			continue
		}
		calls = append(calls, installerCall{
			args:               args,
			releaseOverride:    readFile(t, filepath.Join(dir, "release-override")),
			disableImagePolicy: readFile(t, filepath.Join(dir, "disable-image-policy")),
			installConfig:      readFile(t, filepath.Join(dir, "install-config.yaml")),
		})
	}
	return calls
}

// ocCallCount returns how often the fake oc extracted an installer.
func (e *testEnv) ocCallCount(t *testing.T) int {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(e.logDir, "oc-*"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, dir := range dirs {
		if strings.HasPrefix(readFile(t, filepath.Join(dir, "args")), "adm release extract ") {
			count++
		}
	}
	return count
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
	t.Setenv("HYPERCTL_TEST_RELEASE_INFO_ERROR", "error: unable to read image: unauthorized: authentication required")

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
	if calls := env.installerCalls(t); len(calls) != 0 {
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

	want := "adm release info --registry-config=" + env.pullSecret + " quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64"
	if calls := env.ocCalls(t, want); len(calls) == 0 {
		t.Errorf("expected oc call %q", want)
	}
}

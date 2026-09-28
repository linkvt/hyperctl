package app_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
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
	if want := "--release-image " + pullSpec; !strings.Contains(env.hypershiftCall(t, "create cluster ").args, want) {
		t.Errorf("expected hosted cluster with %q", want)
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

	call := env.hypershiftCall(t, "install ")
	want := "install" +
		" --oidc-storage-provider-s3-bucket-name my-oidc" +
		" --oidc-storage-provider-s3-credentials " + filepath.Join(env.home, ".aws", "credentials") +
		" --oidc-storage-provider-s3-region us-east-2" +
		" --tech-preview-no-upgrade" +
		" --wait-until-available"
	if call.args != want {
		t.Errorf("hypershift args:\ngot:  %s\nwant: %s", call.args, want)
	}
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig"); call.kubeconfig != want {
		t.Errorf("hypershift KUBECONFIG: got %q, want %q", call.kubeconfig, want)
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

	call := env.hypershiftCall(t, "create cluster ")
	want := "^create cluster aws" +
		" --name test-mgmt-workload" +
		" --infra-id test-mgmt-workload-[a-z0-9]{5}" +
		" --region eu-central-1" +
		" --base-domain example.com" +
		" --pull-secret " + regexp.QuoteMeta(env.pullSecret) +
		" --release-image quay\\.io/openshift-release-dev/ocp-release:5\\.1\\.0-ec\\.0-x86_64" +
		" --node-pool-replicas 1" +
		" --auto-node" +
		" --wait" +
		" --timeout 45m0s$"
	if !regexp.MustCompile(want).MatchString(call.args) {
		t.Errorf("hypershift args:\ngot:  %s\nwant: %s", call.args, want)
	}
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig"); call.kubeconfig != want {
		t.Errorf("hypershift KUBECONFIG: got %q, want %q", call.kubeconfig, want)
	}
}

func TestCreateRequiresHypershiftBinary(t *testing.T) {
	env := newTestEnv(t)
	env.moveHypershiftOutOfPath(t)

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
	path := env.moveHypershiftOutOfPath(t)
	configPath := filepath.Join(env.home, ".config", "hyperctl", "config.yaml")
	mustMkdir(t, filepath.Dir(configPath))
	writeFile(t, configPath, "hypershift: "+path+"\n", 0o600)

	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	env.hypershiftCall(t, "create cluster ")
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

	var creates int
	for _, call := range env.hypershiftCalls(t) {
		if strings.HasPrefix(call.args, "create cluster ") {
			creates++
		}
	}
	if creates != 1 {
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

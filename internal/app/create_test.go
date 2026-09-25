package app_test

import (
	"os"
	"path/filepath"
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
	t.Setenv("USER", "tester")
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

func TestCreateRequiresReleaseWhenNotInteractive(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "create", "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "--release") {
		t.Errorf("expected missing --release error, got %v", err)
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
	env.callerARN = "arn:aws:sts::123456789012:assumed-role/123456789012-admin/tester"

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

// installerCall is one recorded invocation of the fake openshift-install.
type installerCall struct {
	args               string
	releaseOverride    string
	disableImagePolicy string
	installConfig      string
}

// installerCalls reads the invocations recorded by the fake openshift-install.
func (e *testEnv) installerCalls(t *testing.T) []installerCall {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(e.logDir, "openshift-install-*"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []installerCall
	for _, dir := range dirs {
		calls = append(calls, installerCall{
			args:               readFile(t, filepath.Join(dir, "args")),
			releaseOverride:    readFile(t, filepath.Join(dir, "release-override")),
			disableImagePolicy: readFile(t, filepath.Join(dir, "disable-image-policy")),
			installConfig:      readFile(t, filepath.Join(dir, "install-config.yaml")),
		})
	}
	return calls
}

// ocCallCount returns how often the fake oc was invoked.
func (e *testEnv) ocCallCount(t *testing.T) int {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(e.logDir, "oc-*"))
	if err != nil {
		t.Fatal(err)
	}
	return len(dirs)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

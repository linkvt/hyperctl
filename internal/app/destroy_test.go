package app_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
)

func TestDestroyHostedDestroysOnlyHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	call := single(t, env.hypershift.destroys)
	want := hypershift.DestroyClusterParams{
		Name:       "test-mgmt-workload",
		InfraID:    env.hostedInfraID(t),
		Region:     "eu-central-1",
		BaseDomain: "example.com",
	}
	if call.params != want {
		t.Errorf("hypershift destroy cluster:\ngot:  %+v\nwant: %+v", call.params, want)
	}
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig"); call.kubeconfig != want {
		t.Errorf("hypershift KUBECONFIG: got %q, want %q", call.kubeconfig, want)
	}
	if _, err := env.runErr(t, "kubeconfig", "--cluster", "test-mgmt-workload"); err == nil {
		t.Error("expected no hosted cluster after destroy")
	}
	env.run(t, "kubeconfig", "--cluster", "test-mgmt")
	if got := env.images.destroys; len(got) != 0 {
		t.Errorf("expected no management cluster destroy, got %v", got)
	}
}

func TestDestroyHostedDestroysInfraWhenManagementClusterIsUnreachable(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	infraID := env.hostedInfraID(t)
	env.cluster.apiDown = true

	env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	if calls := env.hypershift.destroys; len(calls) != 0 {
		t.Errorf("expected no hypershift destroy cluster, got %+v", calls)
	}
	want := hypershift.DestroyClusterParams{
		Name:       "test-mgmt-workload",
		InfraID:    infraID,
		Region:     "eu-central-1",
		BaseDomain: "example.com",
	}
	if calls := env.hypershift.infraDestroys; len(calls) != 1 || calls[0].params != want {
		t.Errorf("hypershift destroy infra:\ngot:  %+v\nwant: %+v", calls, want)
	}
	if _, err := env.runErr(t, "kubeconfig", "--cluster", "test-mgmt-workload"); err == nil {
		t.Error("expected no hosted cluster after destroy")
	}
}

func TestDestroyDestroysHostedAndManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.images.beforeDestroy = func() {
		if len(env.hypershift.destroys) != 1 {
			t.Error("expected the hosted cluster to be destroyed before the management cluster")
		}
	}

	env.run(t, "destroy", "--yes")

	single(t, env.hypershift.destroys)
	calls := env.images.destroys
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install"); len(calls) != 1 || calls[0].installDir != want {
		t.Errorf("installer destroy calls: got %+v, want install dir %s", calls, want)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt")); !os.IsNotExist(err) {
		t.Errorf("expected cluster data to be removed, stat error: %v", err)
	}
	if _, err := env.runErr(t, "kubeconfig", "--cluster", "test-mgmt"); err == nil {
		t.Error("expected no management cluster after destroy")
	}
	if got := env.currentCluster(t); got != "" {
		t.Errorf("current cluster: got %q, want none", got)
	}
}

// TestDestroyCurrentHostedClusterKeepsManagementCluster expects the
// management cluster to become current.
func TestDestroyCurrentHostedClusterKeepsManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.run(t, "use", "test-mgmt-workload")

	env.run(t, "destroy", "--yes")

	single(t, env.hypershift.destroys)
	if got := env.images.destroys; len(got) != 0 {
		t.Errorf("expected no management cluster destroy, got %v", got)
	}
	if got := env.currentCluster(t); got != "test-mgmt" {
		t.Errorf("current cluster: got %q, want test-mgmt", got)
	}
}

func TestDestroyRejectsClusterArguments(t *testing.T) {
	for _, arg := range []string{"hosted", "all", "test-mgmt"} {
		t.Run(arg, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)

			_, err := env.runErr(t, "destroy", arg, "--yes")

			if err == nil || !strings.Contains(err.Error(), "select the cluster with --cluster <name>") {
				t.Errorf("expected a hint to --cluster, got %v", err)
			}
			if destroys := len(env.hypershift.destroys) + len(env.images.destroys); destroys != 0 {
				t.Errorf("expected nothing destroyed, got %d destroys", destroys)
			}
		})
	}
}

func TestDestroyRequiresConfirmationWhenNotInteractive(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	_, err := env.runErr(t, "destroy")

	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("expected confirmation error, got %v", err)
	}
	if got := len(env.images.destroys); got != 0 {
		t.Errorf("expected no destroy calls, got %d", got)
	}
}

func TestDestroyDestroysIncompleteManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.images.installErr = errors.New("openshift-install create cluster: exit status 1")
	if _, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	); err == nil {
		t.Fatal("expected failing installer to fail create")
	}

	env.run(t, "destroy", "--yes")

	if got := len(env.images.destroys); got != 1 {
		t.Errorf("expected 1 installer destroy call, got %d", got)
	}
}

func TestDestroySkipsHostedClusterThatWasNeverCreated(t *testing.T) {
	env := newTestEnv(t)
	env.hypershift.installErr = errors.New("hypershift install: exit status 1")
	if _, err := env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	); err == nil {
		t.Fatal("expected failing hypershift install to fail create")
	}
	env.hypershift.installErr = nil

	env.run(t, "destroy", "--yes")

	if destroys := len(env.hypershift.destroys) + len(env.hypershift.infraDestroys); destroys != 0 {
		t.Errorf("expected no hosted cluster destroy, got %d", destroys)
	}
	if got := len(env.images.destroys); got != 1 {
		t.Errorf("expected 1 installer destroy call, got %d", got)
	}
}

func TestDestroySkipsManagementClusterThatWasNeverInstalled(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
		"--setup", "management",
	)
	// Create stopped between tracking the cluster and recording it.
	clusterDir := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt")
	if err := os.Remove(filepath.Join(clusterDir, "cluster.yaml")); err != nil {
		t.Fatal(err)
	}

	out := env.run(t, "destroy", "--yes")

	if !strings.Contains(out, "Installation never started") {
		t.Errorf("expected nothing to destroy, got:\n%s", out)
	}
	if got := len(env.images.destroys); got != 0 {
		t.Errorf("expected no installer destroy call, got %d", got)
	}
	if _, err := os.Stat(clusterDir); !os.IsNotExist(err) {
		t.Errorf("expected the cluster directory to be removed, got %v", err)
	}
}

func TestDestroySkipsHostedClusterWithoutRecord(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)
	// Create stopped between tracking the hosted cluster and recording it.
	if err := os.Remove(filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/hosted/test-mgmt-workload/cluster.yaml")); err != nil {
		t.Fatal(err)
	}

	out := env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	if !strings.Contains(out, "Creation never started") {
		t.Errorf("expected nothing to destroy, got:\n%s", out)
	}
	if destroys := len(env.hypershift.destroys) + len(env.hypershift.infraDestroys); destroys != 0 {
		t.Errorf("expected no hosted cluster destroy, got %d", destroys)
	}
}

// TestDestroyClustersRecordedByEarlierVersions destroys clusters from the
// records in testdata/data, which earlier versions of hyperctl wrote.
func TestDestroyClustersRecordedByEarlierVersions(t *testing.T) {
	env := newTestEnv(t)
	data := filepath.Join(env.home, ".local/share/hyperctl")
	if err := os.CopyFS(data, os.DirFS(filepath.Join("testdata", "data"))); err != nil {
		t.Fatal(err)
	}

	env.run(t, "destroy", "--cluster", "aws-mgmt", "--yes")

	wantHosted := hypershift.DestroyClusterParams{Name: "aws-mgmt-workload", InfraID: "aws-mgmt-workload-abcde", Region: "eu-central-1", BaseDomain: "example.com"}
	if got := single(t, env.hypershift.destroys).params; got != wantHosted {
		t.Errorf("hypershift destroy cluster:\ngot:  %+v\nwant: %+v", got, wantHosted)
	}
	if len(env.images.destroys) != 1 {
		t.Fatalf("expected 1 installer destroy call, got %+v", env.images.destroys)
	}
	if got, want := env.images.destroys[0].installDir, filepath.Join(data, "clusters/aws-mgmt/install"); got != want {
		t.Errorf("installer destroy dir: got %s, want %s", got, want)
	}
	// The installer for the recorded release was extracted to destroy.
	installer := fakeInstallerPath(filepath.Join(env.home, ".cache/hyperctl/releases"), ocp.ParseRelease("5.1.0-ec.0"))
	if _, err := os.Stat(installer.Path); err != nil {
		t.Errorf("expected the installer of the recorded release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "clusters/aws-mgmt")); !os.IsNotExist(err) {
		t.Errorf("expected the cluster directory to be removed, got %v", err)
	}
}

func TestCreateAfterDestroyHostedReusesManagementClusterSettings(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	env.run(t, "create")

	// Each creation gets a new infra ID.
	var creates []hypershift.CreateClusterParams
	for _, call := range env.hypershift.clusters {
		params := call.params
		params.InfraID = ""
		creates = append(creates, params)
	}
	if len(creates) != 2 || creates[1] != creates[0] {
		t.Errorf("expected the hosted cluster to be recreated with the same parameters, got %+v", creates)
	}
}

// createClusters creates a management and a hosted cluster.
func (e *testEnv) createClusters(t *testing.T) {
	t.Helper()
	e.run(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--base-domain", "example.com",
		"--name", "test-mgmt",
		"--pull-secret", e.pullSecret,
	)
}

// currentCluster returns the current cluster from the state.
func (e *testEnv) currentCluster(t *testing.T) string {
	t.Helper()
	var st struct {
		Current string `json:"current"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, filepath.Join(e.home, ".local/state/hyperctl/state.yaml"))), &st); err != nil {
		t.Fatal(err)
	}
	return st.Current
}

// hostedInfraID returns the infra ID the hosted cluster was created with.
func (e *testEnv) hostedInfraID(t *testing.T) string {
	t.Helper()
	if clusters := e.hypershift.clusters; len(clusters) > 0 {
		return clusters[0].params.InfraID
	}
	if clusters := e.hypershift.azureClusters; len(clusters) > 0 {
		return clusters[0].params.InfraID
	}
	t.Fatal("no hosted cluster created")
	return ""
}

func TestDestroySharedAWSDeletesEmptyOIDCBucket(t *testing.T) {
	env := newTestEnv(t)
	env.aws.publicBuckets["tester-hypershift-oidc"] = "eu-west-1"

	env.run(t, "destroy", "shared", "--platform", "aws", "--yes")

	if want := []deletedBucket{{"tester-hypershift-oidc", "eu-west-1"}}; !slices.Equal(env.aws.deletedBuckets, want) {
		t.Errorf("deleted buckets: got %v, want %v", env.aws.deletedBuckets, want)
	}
	if len(env.azure.deletedApplications)+len(env.azure.deletedResourceGroups) != 0 {
		t.Errorf("expected no Azure resources deleted, got %v and %v", env.azure.deletedApplications, env.azure.deletedResourceGroups)
	}
}

func TestDestroySharedAWSDeletesConfiguredOIDCBucket(t *testing.T) {
	env := newTestEnv(t)
	mustMkdir(t, filepath.Join(env.home, ".config", "hyperctl"))
	writeFile(t, filepath.Join(env.home, ".config", "hyperctl", "config.yaml"), "oidcBucket: team-oidc\n", 0o600)
	env.aws.publicBuckets["team-oidc"] = "us-east-1"
	env.aws.publicBuckets["tester-hypershift-oidc"] = "us-east-1"

	env.run(t, "destroy", "shared", "--platform", "aws", "--yes")

	if want := []deletedBucket{{"team-oidc", "us-east-1"}}; !slices.Equal(env.aws.deletedBuckets, want) {
		t.Errorf("deleted buckets: got %v, want %v", env.aws.deletedBuckets, want)
	}
}

// TestDestroySharedAWSKeepsOIDCBucketWithDocuments keeps a bucket that still
// holds OIDC documents, which hosted clusters outside of hyperctl may use.
func TestDestroySharedAWSKeepsOIDCBucketWithDocuments(t *testing.T) {
	env := newTestEnv(t)
	env.aws.publicBuckets["tester-hypershift-oidc"] = "eu-west-1"
	env.aws.bucketObjects = map[string][]string{"tester-hypershift-oidc": {
		"other-abc12/.well-known/openid-configuration",
		"other-abc12/openid/v1/jwks",
	}}

	_, err := env.runErr(t, "destroy", "shared", "--platform", "aws", "--yes")

	if err == nil || !strings.Contains(err.Error(), "other-abc12") {
		t.Errorf("expected an error naming the documents, got %v", err)
	}
	if len(env.aws.deletedBuckets) != 0 {
		t.Errorf("expected no bucket deleted, got %v", env.aws.deletedBuckets)
	}
}

func TestDestroySharedAWSWithoutOIDCBucket(t *testing.T) {
	env := newTestEnv(t)

	out := env.run(t, "destroy", "shared", "--platform", "aws", "--yes")

	if !strings.Contains(out, "OIDC bucket tester-hypershift-oidc does not exist") {
		t.Errorf("expected the missing bucket to be reported, got:\n%s", out)
	}
	if len(env.aws.deletedBuckets) != 0 {
		t.Errorf("expected no bucket deleted, got %v", env.aws.deletedBuckets)
	}
}

func TestDestroySharedAWSRequiresConfirmation(t *testing.T) {
	env := newTestEnv(t)
	env.aws.publicBuckets["tester-hypershift-oidc"] = "eu-west-1"

	_, err := env.runErr(t, "destroy", "shared", "--platform", "aws")

	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("expected an error asking for --yes, got %v", err)
	}
	if len(env.aws.deletedBuckets) != 0 {
		t.Errorf("expected no bucket deleted, got %v", env.aws.deletedBuckets)
	}
}

func TestDestroySharedRejectsFurtherArguments(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "destroy", "shared", "bogus", "--platform", "aws", "--yes")

	if err == nil || !strings.Contains(err.Error(), `unexpected argument "bogus"`) {
		t.Errorf("expected the argument to be rejected, got %v", err)
	}
}

func TestDestroySharedRequiresPlatform(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "destroy", "shared", "--yes")

	if err == nil || !strings.Contains(err.Error(), "--platform") {
		t.Errorf("expected an error asking for --platform, got %v", err)
	}
}

// TestDestroySharedWithYesRequiresPlatform expects --yes to fail instead of
// asking for the platform in a terminal.
func TestDestroySharedWithYesRequiresPlatform(t *testing.T) {
	env := newTestEnv(t)
	env.interactive = true

	_, err := env.runErr(t, "destroy", "shared", "--yes")

	if err == nil || !strings.Contains(err.Error(), "--platform") {
		t.Errorf("expected an error asking for --platform, got %v", err)
	}
}

// TestDestroySharedIgnoresClustersOfOtherPlatforms destroys the shared Azure
// resources while an AWS management cluster exists.
func TestDestroySharedIgnoresClustersOfOtherPlatforms(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create", "--setup", "management", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret)

	env.run(t, "destroy", "shared", "--platform", "azure", "--yes")

	if !slices.Contains(env.azure.deletedResourceGroups, "tester-hyperctl") {
		t.Errorf("expected the shared resource group to be deleted, got %v", env.azure.deletedResourceGroups)
	}
}

func TestDestroyRejectsPlatformForClusters(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create", "--setup", "management", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret)

	_, err := env.runErr(t, "destroy", "--platform", "aws", "--yes")

	if err == nil || !strings.Contains(err.Error(), "--platform only applies to destroy shared") {
		t.Errorf("expected --platform to be rejected, got %v", err)
	}
	if len(env.images.destroys) != 0 {
		t.Errorf("expected no cluster destroyed, got %v", env.images.destroys)
	}
}

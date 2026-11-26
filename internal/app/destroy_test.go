package app_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkvt/hyperctl/internal/hypershift"
)

func TestDestroyHostedDestroysOnlyHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "destroy", "hosted", "--yes")

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
	if _, err := env.runErr(t, "kubeconfig", "hosted"); err == nil {
		t.Error("expected no hosted cluster after destroy")
	}
	env.run(t, "kubeconfig", "mgmt")
	if got := env.images.destroys; len(got) != 0 {
		t.Errorf("expected no management cluster destroy, got %v", got)
	}
}

func TestDestroyHostedDestroysInfraWhenManagementClusterIsUnreachable(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	infraID := env.hostedInfraID(t)
	t.Setenv("HYPERCTL_TEST_API_DOWN", "1")

	env.run(t, "destroy", "hosted", "--yes")

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
	if _, err := env.runErr(t, "kubeconfig", "hosted"); err == nil {
		t.Error("expected no hosted cluster after destroy")
	}
}

func TestDestroyDestroysHostedAndManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "destroy", "--yes")

	single(t, env.hypershift.destroys)
	calls := env.images.destroys
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install"); len(calls) != 1 || calls[0].installDir != want {
		t.Errorf("installer destroy calls: got %+v, want install dir %s", calls, want)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt")); !os.IsNotExist(err) {
		t.Errorf("expected cluster data to be removed, stat error: %v", err)
	}
	if _, err := env.runErr(t, "kubeconfig", "mgmt"); err == nil {
		t.Error("expected no management cluster after destroy")
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

func TestCreateAfterDestroyHostedReusesManagementClusterSettings(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.run(t, "destroy", "hosted", "--yes")

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

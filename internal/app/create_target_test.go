package app_test

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestCreateClustersOnDifferentPlatforms creates an AKS cluster next to an
// OpenShift management cluster with a hosted cluster.
func TestCreateClustersOnDifferentPlatforms(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "create", "--platform", "azure", "--setup", "standalone", "--region", testAzureLocation, "--name", "test-aks")

	want := "" +
		"  NAME                   PLATFORM  SETUP       PHASE\n" +
		"* test-aks               azure     standalone  Installed\n" +
		"  test-mgmt              aws       management  Installed\n" +
		"  └─ test-mgmt-workload                        Installed\n"
	if out := env.run(t, "list"); out != want {
		t.Errorf("list:\ngot:\n%s\nwant:\n%s", out, want)
	}
	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected the OpenShift cluster to be installed once, got %d", got)
	}
	// --cluster selects another than the current cluster.
	if paths := strings.Split(strings.TrimSpace(env.run(t, "kubeconfig")), ":"); len(paths) != 1 || !strings.Contains(paths[0], "/clusters/test-aks/") {
		t.Errorf("kubeconfig of the current cluster: got %v", paths)
	}
	if paths := strings.Split(strings.TrimSpace(env.run(t, "kubeconfig", "--cluster", "test-mgmt")), ":"); len(paths) != 2 {
		t.Errorf("kubeconfig of test-mgmt: got %v, want the hosted and management cluster", paths)
	}
}

// TestCreateNewClusterTakesPlatformFromFlag expects a new cluster not to
// take the platform of the current cluster.
func TestCreateNewClusterTakesPlatformFromFlag(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	env.run(t, "create", "--setup", "management", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-aws", "--pull-secret", env.pullSecret)

	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected an OpenShift cluster on aws, got %d installations", got)
	}
	if got := len(env.azure.aksClusters); got != 1 {
		t.Errorf("expected no second AKS cluster, got %d", got)
	}
}

// TestCreateAddsToCurrentCluster expects create without --cluster or
// --name to add a hosted cluster to the current management cluster, named
// after the first free <name>-workload.
func TestCreateAddsToCurrentCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	out := env.run(t, "create")

	if !strings.Contains(out, "Platform aws (management cluster test-mgmt)") {
		t.Errorf("expected the platform of test-mgmt, got:\n%s", out)
	}
	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected the management cluster to be installed once, got %d", got)
	}
	if got := len(env.hypershift.installs); got != 2 {
		t.Errorf("expected the HyperShift operator to be installed again, got %d installs", got)
	}
	var names []string
	for _, c := range env.hypershift.clusters {
		names = append(names, c.params.Name)
	}
	if want := []string{"test-mgmt-workload", "test-mgmt-workload-2"}; !slices.Equal(names, want) {
		t.Errorf("hosted clusters: got %v, want %v", names, want)
	}
	if got := env.hypershift.clusters[1].params.ReleaseImage; got != "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64" {
		t.Errorf("expected the release of the management cluster, got %s", got)
	}
}

// TestCreateAddsToManagementClusterOfCurrentHostedCluster expects a current
// hosted cluster to add to its management cluster.
func TestCreateAddsToManagementClusterOfCurrentHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.run(t, "use", "test-mgmt-workload")

	env.run(t, "create", "--hosted-name", "second")

	if got := len(env.hypershift.clusters); got != 2 {
		t.Fatalf("expected a second hosted cluster, got %d", got)
	}
	if got := env.hypershift.clusters[1].params.Name; got != "second" {
		t.Errorf("hosted cluster: got %s, want second", got)
	}
}

// TestCreateAddsToManagementClusterOfHostedCluster expects --cluster of a
// hosted cluster to add to its management cluster, also if the creation of
// the hosted cluster did not complete.
func TestCreateAddsToManagementClusterOfHostedCluster(t *testing.T) {
	tests := map[string]struct {
		createClusterErr error
	}{
		"installed hosted cluster": {},
		"incomplete hosted cluster": {
			createClusterErr: errors.New("hypershift create cluster: exit status 1"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.hypershift.createClusterErr = tc.createClusterErr
			_, _ = env.runErr(t, "create", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret)
			env.hypershift.createClusterErr = nil

			env.run(t, "create", "--cluster", "test-mgmt-workload")

			if got := len(env.hypershift.clusters); got != 2 {
				t.Fatalf("expected a second hosted cluster, got %d", got)
			}
			if got := env.hypershift.clusters[1].params.Name; got != "test-mgmt-workload-2" {
				t.Errorf("hosted cluster: got %s, want test-mgmt-workload-2", got)
			}
		})
	}
}

// TestCreateAddsToClusterOnAnotherPlatform expects --cluster of a cluster on
// another platform than the current one to take that cluster's platform and
// recorded values.
func TestCreateAddsToClusterOnAnotherPlatform(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	env.run(t, "create", "--setup", "management", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-aws", "--pull-secret", env.pullSecret)

	out := env.run(t, "create", "--cluster", "test-mgmt")

	if !strings.Contains(out, "Platform azure (management cluster test-mgmt)") {
		t.Errorf("expected the platform of test-mgmt, got:\n%s", out)
	}
	if got := len(env.azure.aksClusters); got != 1 {
		t.Errorf("expected the AKS cluster to be kept, got %d", got)
	}
	if got := len(env.hypershift.azureClusters); got != 2 {
		t.Errorf("expected a second hosted cluster, got %d", got)
	}
	if got := env.currentCluster(t); got != "test-mgmt" {
		t.Errorf("current cluster: got %q, want test-mgmt", got)
	}
}

// TestCreateAddsOnlyOperator expects setup management to install the
// HyperShift operator on an existing cluster without creating a hosted
// cluster.
func TestCreateAddsOnlyOperator(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "create", "--cluster", "test-mgmt", "--setup", "management")

	if got := len(env.hypershift.installs); got != 2 {
		t.Errorf("expected the HyperShift operator to be installed again, got %d installs", got)
	}
	if got := len(env.hypershift.clusters); got != 1 {
		t.Errorf("expected no further hosted cluster, got %d", got)
	}
	// The last used setup is the default of new clusters.
	if st := readFile(t, filepath.Join(env.home, ".local/state/hyperctl/state.yaml")); !strings.Contains(st, "setup: hosted") {
		t.Errorf("expected the setup of new clusters to be kept, got:\n%s", st)
	}
}

// TestCreateAddsOperatorWithHostedCluster expects a hosted cluster added to
// a standalone cluster to install the HyperShift operator first.
func TestCreateAddsOperatorWithHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create", "--setup", "standalone", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret)

	env.run(t, "create", "--cluster", "test-mgmt")

	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected the cluster to be installed once, got %d installations", got)
	}
	if got := len(env.hypershift.installs); got != 1 {
		t.Errorf("expected the HyperShift operator to be installed, got %d installs", got)
	}
	if got := single(t, env.hypershift.clusters).params.Name; got != "test-mgmt-workload" {
		t.Errorf("hosted cluster: got %s, want test-mgmt-workload", got)
	}
	record := readFile(t, filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/cluster.yaml"))
	if !strings.Contains(record, "hypershiftOperator: true") {
		t.Errorf("expected the record to note the HyperShift operator, got:\n%s", record)
	}
}

// TestCreateWithoutInstalledCurrentClusterCreatesNewCluster expects create
// not to add to a current cluster whose installation did not complete.
func TestCreateWithoutInstalledCurrentClusterCreatesNewCluster(t *testing.T) {
	env := newTestEnv(t)
	env.images.installErr = errors.New("openshift-install create cluster: exit status 1")
	if _, err := env.runErr(t, "create", "--setup", "standalone", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret); err == nil {
		t.Fatal("expected failing installer to fail create")
	}
	env.images.installErr = nil

	env.run(t, "create", "--setup", "standalone", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--pull-secret", env.pullSecret)

	if got := env.currentCluster(t); got != "tester-karpenter" {
		t.Errorf("current cluster: got %q, want tester-karpenter", got)
	}
}

func TestCreateRejectsTargets(t *testing.T) {
	tests := map[string]struct {
		args []string
		want string
	}{
		"cluster and name": {
			args: []string{"--cluster", "test-mgmt", "--name", "other"},
			want: "--cluster adds to an existing cluster, --name names a new one, set only one",
		},
		"unknown cluster": {
			args: []string{"--cluster", "other"},
			want: "cluster other does not exist, clusters: test-mgmt, test-mgmt-workload",
		},
		"name of a management cluster": {
			args: []string{"--name", "test-mgmt"},
			want: "test-mgmt exists, add to it with --cluster test-mgmt or choose another --name",
		},
		"name of a hosted cluster": {
			args: []string{"--name", "test-mgmt-workload"},
			want: "test-mgmt-workload exists, add to it with --cluster test-mgmt-workload or choose another --name",
		},
		"hosted name of a management cluster": {
			args: []string{"--name", "other", "--hosted-name", "test-mgmt"},
			want: "test-mgmt exists, choose another --hosted-name",
		},
		"hosted name of another management cluster's hosted cluster": {
			args: []string{"--name", "other", "--hosted-name", "test-mgmt-workload"},
			want: "test-mgmt-workload exists, choose another --hosted-name",
		},
		"hosted name of an existing hosted cluster": {
			args: []string{"--cluster", "test-mgmt", "--hosted-name", "test-mgmt-workload"},
			want: "test-mgmt-workload exists, choose another --hosted-name",
		},
		"hosted name equal to the name": {
			args: []string{"--name", "other", "--hosted-name", "other"},
			want: "--hosted-name other equals --name, choose another --hosted-name",
		},
		"standalone setup for an existing cluster": {
			args: []string{"--cluster", "test-mgmt", "--setup", "standalone"},
			want: "--setup standalone adds nothing to an existing cluster, expected hosted or management, or create a new cluster with --name",
		},
		"instance type for an existing cluster": {
			args: []string{"--cluster", "test-mgmt", "--instance-type", "m6i.2xlarge"},
			want: "--instance-type only applies to new clusters, create one with --name",
		},
		"workers for an existing cluster": {
			args: []string{"--cluster", "test-mgmt", "--workers", "1"},
			want: "--workers only applies to new clusters, create one with --name",
		},
		"platform of an existing cluster": {
			args: []string{"--cluster", "test-mgmt", "--platform", "azure"},
			want: "management cluster test-mgmt runs on aws, create a cluster on azure with --name",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)

			out, err := env.runErr(t, append([]string{"create", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--pull-secret", env.pullSecret}, tc.args...)...)

			if err == nil || err.Error() != tc.want {
				t.Errorf("got %v, want %q", err, tc.want)
			}
			if strings.Contains(out, "AWS identity") {
				t.Errorf("expected no account check, got:\n%s", out)
			}
		})
	}
}

func TestCreateRejectsEmptyNames(t *testing.T) {
	tests := map[string]struct {
		args []string
		want string
	}{
		"name": {
			args: []string{"--name", ""},
			want: "--name must not be empty",
		},
		"cluster": {
			args: []string{"--cluster", ""},
			want: "--cluster must not be empty",
		},
		"hosted name": {
			args: []string{"--cluster", "test-mgmt", "--hosted-name", ""},
			want: "--hosted-name must not be empty",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.run(t, "create", "--setup", "management", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret)

			out, err := env.runErr(t, append([]string{"create", "--setup", "hosted", "--release", "5.2.0"}, tc.args...)...)

			if err == nil || err.Error() != tc.want {
				t.Errorf("got %v, want %q", err, tc.want)
			}
			if strings.Contains(out, "AWS identity") {
				t.Errorf("expected no account check, got:\n%s", out)
			}
			if got := len(env.hypershift.clusters); got != 0 {
				t.Errorf("expected no hosted cluster, got %d", got)
			}
		})
	}
}

func TestCreateRejectsFlagsThatDifferFromExistingCluster(t *testing.T) {
	tests := map[string]struct {
		args []string
		want string
	}{
		"region": {
			args: []string{"--region", "eu-west-1"},
			want: "--region eu-west-1 differs from eu-central-1 of management cluster test-mgmt",
		},
		"base domain": {
			args: []string{"--base-domain", "other.example.com"},
			want: "--base-domain other.example.com differs from example.com of management cluster test-mgmt",
		},
		"release without a cluster to create": {
			args: []string{"--setup", "management", "--release", "5.2.0"},
			want: "--release only applies to clusters this run creates, management cluster test-mgmt exists",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)

			_, err := env.runErr(t, append([]string{"create", "--cluster", "test-mgmt"}, tc.args...)...)

			if err == nil || err.Error() != tc.want {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCreateAcceptsFlagsThatMatchExistingCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "create", "--cluster", "test-mgmt", "--setup", "management", "--region", "eu-central-1", "--base-domain", "example.com", "--pull-secret", env.pullSecret)

	if got := len(env.hypershift.clusters); got != 1 {
		t.Errorf("expected no further hosted cluster, got %d", got)
	}
}

// TestCreateRejectsIncompleteClusters expects clusters whose creation did
// not complete to be destroyed before creating them again or adding to them.
func TestCreateRejectsIncompleteClusters(t *testing.T) {
	tests := map[string]struct {
		installErr, createClusterErr error
		args                         []string
		want                         string
	}{
		"new management cluster": {
			installErr: errors.New("openshift-install create cluster: exit status 1"),
			args:       []string{"--name", "test-mgmt"},
			want:       "previous installation of management cluster test-mgmt did not complete, destroy it first",
		},
		"add to management cluster": {
			installErr: errors.New("openshift-install create cluster: exit status 1"),
			args:       []string{"--cluster", "test-mgmt"},
			want:       "previous installation of management cluster test-mgmt did not complete, destroy it first",
		},
		"hosted cluster": {
			createClusterErr: errors.New("hypershift create cluster: exit status 1"),
			args:             []string{"--cluster", "test-mgmt", "--hosted-name", "test-mgmt-workload"},
			want:             "previous creation of hosted cluster test-mgmt-workload did not complete, destroy it first",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.images.installErr = tc.installErr
			env.hypershift.createClusterErr = tc.createClusterErr
			if _, err := env.runErr(t, "create", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret); err == nil {
				t.Fatal("expected the first create to fail")
			}
			env.images.installErr, env.hypershift.createClusterErr = nil, nil

			_, err := env.runErr(t, append([]string{"create", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--pull-secret", env.pullSecret}, tc.args...)...)

			if err == nil || err.Error() != tc.want {
				t.Errorf("got %v, want %q", err, tc.want)
			}
			if got := env.currentCluster(t); got != "test-mgmt" {
				t.Errorf("current cluster: got %q, want test-mgmt", got)
			}
		})
	}
}

// TestCreateReusesNameOfClusterWithoutRecord expects a cluster whose
// creation stopped before its record to be created again under its name.
func TestCreateReusesNameOfClusterWithoutRecord(t *testing.T) {
	env := newTestEnv(t)
	args := []string{"create", "--setup", "standalone", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-mgmt", "--pull-secret", env.pullSecret}
	if _, err := env.runErr(t, append(args, "--ssh-key", filepath.Join(env.home, "missing.pub"))...); err == nil {
		t.Fatal("expected create to fail without the SSH key")
	}

	env.run(t, args...)

	if got := len(env.images.creates); got != 1 || filepath.Base(filepath.Dir(env.images.creates[0].installDir)) != "test-mgmt" {
		t.Errorf("expected test-mgmt to be installed, got %+v", env.images.creates)
	}
}

func TestCreateSecondHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.createSecondHostedCluster(t)

	if len(env.hypershift.clusters) != 2 {
		t.Fatalf("expected two hosted clusters, got %+v", env.hypershift.clusters)
	}
	second := env.hypershift.clusters[1].params
	if second.Name != "second" || second.ReleaseImage != "quay.io/openshift-release-dev/ocp-release:5.2.0-x86_64" || second.Region != "eu-central-1" {
		t.Errorf("second hosted cluster: got %+v", second)
	}
	if got := len(env.images.creates); got != 1 {
		t.Errorf("expected the management cluster to be installed once, got %d", got)
	}
	record := readFile(t, filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/cluster.yaml"))
	if !strings.Contains(record, "release: 5.1.0-ec.0") {
		t.Errorf("expected the management cluster to keep its release, got:\n%s", record)
	}
	if got := env.currentCluster(t); got != "test-mgmt" {
		t.Errorf("current cluster: got %q, want test-mgmt", got)
	}
}

func TestKubeconfigOfManagementClusterWithSeveralHostedClusters(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.createSecondHostedCluster(t)

	out := env.run(t, "kubeconfig")

	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig") + "\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if want := "select one with --cluster: second, test-mgmt-workload"; !strings.Contains(env.stderr, want) {
		t.Errorf("expected stderr to contain %q, got %q", want, env.stderr)
	}
}

func TestImageRequiresSelectedHostedClusterOfSeveral(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.createSecondHostedCluster(t)

	_, err := env.runErr(t, "image", "karpenter-operator", "quay.io/test/karpenter-operator:dev")

	if want := "management cluster test-mgmt has several hosted clusters, select one with --cluster: second, test-mgmt-workload"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
	env.run(t, "image", "karpenter-operator", "--cluster", "second", "quay.io/test/karpenter-operator:dev")
	if got := env.cluster.annotations[fakeObject{kind: "hostedcluster", namespace: "clusters", name: "second"}]; len(got) != 1 {
		t.Errorf("expected the override on second, got %v", got)
	}
}

func TestStatusOfHostedClusterShowsItsManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.createSecondHostedCluster(t)

	out := env.run(t, "status", "--cluster", "second")

	if !strings.Contains(out, "Management cluster test-mgmt\n") || !strings.Contains(out, "Hosted cluster second\n") {
		t.Errorf("expected the management cluster and second, got:\n%s", out)
	}
	if strings.Contains(out, "test-mgmt-workload") {
		t.Errorf("expected no other hosted cluster, got:\n%s", out)
	}
	if all := env.run(t, "status"); !strings.Contains(all, "Hosted cluster second\n") || !strings.Contains(all, "Hosted cluster test-mgmt-workload\n") {
		t.Errorf("expected the management cluster with both hosted clusters, got:\n%s", all)
	}
}

func TestDestroyManagementClusterWithSeveralHostedClusters(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.createSecondHostedCluster(t)
	env.images.beforeDestroy = func() {
		if len(env.hypershift.destroys) != 2 {
			t.Error("expected the hosted clusters to be destroyed before the management cluster")
		}
	}

	env.run(t, "destroy", "--yes")

	var destroyed []string
	for _, call := range env.hypershift.destroys {
		destroyed = append(destroyed, call.params.Name)
	}
	if want := []string{"second", "test-mgmt-workload"}; !slices.Equal(destroyed, want) {
		t.Errorf("destroyed hosted clusters: got %v, want %v", destroyed, want)
	}
	if out := env.run(t, "list"); !strings.Contains(out, "No clusters") {
		t.Errorf("expected no clusters, got:\n%s", out)
	}
}

func TestDestroyHostedClusterKeepsOtherHostedClusters(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	env.createSecondHostedCluster(t)

	env.run(t, "destroy", "--cluster", "second", "--yes")

	if got := single(t, env.hypershift.destroys).params.Name; got != "second" {
		t.Errorf("destroyed hosted cluster: got %s, want second", got)
	}
	want := "" +
		"  NAME                   PLATFORM  SETUP       PHASE\n" +
		"* test-mgmt              aws       management  Installed\n" +
		"  └─ test-mgmt-workload                        Installed\n"
	if out := env.run(t, "list"); out != want {
		t.Errorf("list:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// TestCreateKeepsStateChangesOfConcurrentRuns creates an AKS cluster while
// another create prepares an AWS cluster.
func TestCreateKeepsStateChangesOfConcurrentRuns(t *testing.T) {
	env := newTestEnv(t)
	env.aws.beforeClusterDNS = func() {
		env.aws.beforeClusterDNS = nil
		env.run(t, "create", "--platform", "azure", "--setup", "standalone", "--region", "germanywestcentral", "--name", "test-aks")
	}

	env.run(t, "create", "--setup", "standalone", "--release", "5.1.0-ec.0", "--region", "eu-central-1", "--name", "test-aws", "--pull-secret", env.pullSecret)

	var st struct {
		Current     string            `json:"current"`
		LastRegions map[string]string `json:"lastRegions"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, filepath.Join(env.home, ".local/state/hyperctl/state.yaml"))), &st); err != nil {
		t.Fatal(err)
	}
	if st.Current != "test-aws" || st.LastRegions["aws"] != "eu-central-1" || st.LastRegions["azure"] != "germanywestcentral" {
		t.Errorf("state: got %+v, want current test-aws and the regions of both runs", st)
	}
}

// createSecondHostedCluster adds the hosted cluster second with another
// release to the management cluster of createClusters.
func (e *testEnv) createSecondHostedCluster(t *testing.T) {
	t.Helper()
	e.run(t, "create", "--cluster", "test-mgmt", "--hosted-name", "second", "--release", "5.2.0")
}

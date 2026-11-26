package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func TestManagementClusterPaths(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: "/data"})
	c := managementCluster("mgmt")

	for name, tc := range map[string]struct{ got, want string }{
		"dir":         {store.dir(c), "/data/clusters/mgmt"},
		"install dir": {store.installDir(c), "/data/clusters/mgmt/install"},
		"kubeconfig":  {store.kubeconfig(c), "/data/clusters/mgmt/install/auth/kubeconfig"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %s, want %s", name, tc.got, tc.want)
		}
	}
}

func TestHostedClusterPaths(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: "/data"})
	c := hostedCluster("mgmt", "workload")

	for name, tc := range map[string]struct{ got, want string }{
		"dir":        {store.dir(c), "/data/clusters/mgmt/hosted/workload"},
		"kubeconfig": {store.kubeconfig(c), "/data/clusters/mgmt/hosted/workload/kubeconfig"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %s, want %s", name, tc.got, tc.want)
		}
	}
}

func TestClusterStoreCheck(t *testing.T) {
	tests := map[string]struct {
		cluster       clusterRef
		phase         clusterPhase
		wantInstalled bool
		wantError     string
	}{
		"new management cluster": {cluster: managementCluster("mgmt")},
		"installed management cluster": {
			cluster:       managementCluster("mgmt"),
			phase:         phaseInstalled,
			wantInstalled: true,
		},
		"incomplete management cluster": {
			cluster:   managementCluster("mgmt"),
			phase:     phaseInstalling,
			wantError: "previous installation of management cluster mgmt did not complete, destroy it first",
		},
		"installed hosted cluster": {
			cluster:       hostedCluster("mgmt", "workload"),
			phase:         phaseInstalled,
			wantInstalled: true,
		},
		"incomplete hosted cluster": {
			cluster:   hostedCluster("mgmt", "workload"),
			phase:     phaseInstalling,
			wantError: "previous creation of hosted cluster workload did not complete, destroy it first",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
			if tc.phase != "" {
				if err := store.save(tc.cluster, clusterRecord{Platform: platformAWS, Phase: tc.phase}); err != nil {
					t.Fatal(err)
				}
			}

			installed, err := store.check(tc.cluster)

			checkError(t, err, tc.wantError)
			if installed != tc.wantInstalled {
				t.Errorf("installed: got %v, want %v", installed, tc.wantInstalled)
			}
		})
	}
}

func TestClusterStoreBeginRequiresFields(t *testing.T) {
	tests := map[string]struct {
		cluster   clusterRef
		record    clusterRecord
		wantError string
	}{
		"OpenShift management cluster": {
			cluster: managementCluster("mgmt"),
			record:  clusterRecord{Platform: platformAWS, PullSecret: "/pull-secret.json"},
		},
		"OpenShift management cluster without pull secret": {
			cluster:   managementCluster("mgmt"),
			record:    clusterRecord{Platform: platformAWS},
			wantError: "management cluster mgmt needs the pull secret",
		},
		"AKS management cluster": {
			cluster: managementCluster("mgmt"),
			record:  clusterRecord{Platform: platformAzure},
		},
		"AWS hosted cluster": {
			cluster: hostedCluster("mgmt", "workload"),
			record:  clusterRecord{Platform: platformAWS, InfraID: "workload-abcde"},
		},
		"hosted cluster without infra ID": {
			cluster:   hostedCluster("mgmt", "workload"),
			record:    clusterRecord{Platform: platformAWS},
			wantError: "hosted cluster workload needs an infra ID",
		},
		"Azure hosted cluster": {
			cluster: hostedCluster("mgmt", "workload"),
			record:  clusterRecord{Platform: platformAzure, InfraID: "workload-abcde", Azure: &azureRecord{ResourceGroup: "shared"}},
		},
		"Azure hosted cluster without shared resource group": {
			cluster:   hostedCluster("mgmt", "workload"),
			record:    clusterRecord{Platform: platformAzure, InfraID: "workload-abcde", Azure: &azureRecord{}},
			wantError: "hosted cluster workload needs the shared Azure resources",
		},
		"Azure hosted cluster without shared resources": {
			cluster:   hostedCluster("mgmt", "workload"),
			record:    clusterRecord{Platform: platformAzure, InfraID: "workload-abcde"},
			wantError: "hosted cluster workload needs the shared Azure resources",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store := newClusterStore(xdg.Dirs{Data: t.TempDir()})

			err := store.begin(tc.cluster, tc.record)

			checkError(t, err, tc.wantError)
			_, found, loadErr := store.load(tc.cluster)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if found != (tc.wantError == "") {
				t.Errorf("record saved: got %v, want %v", found, tc.wantError == "")
			}
		})
	}
}

func TestClusterStoreBeginSavesInstalling(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
	c := hostedCluster("mgmt", "workload")

	if err := store.begin(c, clusterRecord{Platform: platformAWS, Release: "5.1.0-ec.0", InfraID: "workload-abcde"}); err != nil {
		t.Fatal(err)
	}

	record, _, err := store.load(c)
	if err != nil {
		t.Fatal(err)
	}
	if record.Phase != phaseInstalling || record.Release != "5.1.0-ec.0" || record.UpdatedAt.IsZero() {
		t.Errorf("expected an Installing record with the release and update time, got %+v", record)
	}
}

func TestClusterStoreBeginRefusesExistingRecord(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
	c := hostedCluster("mgmt", "workload")
	record := clusterRecord{Platform: platformAWS, InfraID: "workload-abcde"}
	if err := store.begin(c, record); err != nil {
		t.Fatal(err)
	}

	err := store.begin(c, record)

	checkError(t, err, "hosted cluster workload already has a record")
}

func TestClusterStoreMarkInstalled(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
	c := managementCluster("mgmt")
	if err := store.begin(c, clusterRecord{Platform: platformAzure, Region: "westus3"}); err != nil {
		t.Fatal(err)
	}

	if err := store.markInstalled(c); err != nil {
		t.Fatal(err)
	}

	installed, err := store.check(c)
	if err != nil || !installed {
		t.Errorf("expected an installed cluster, got installed=%v err=%v", installed, err)
	}
	if record, _, _ := store.load(c); record.Region != "westus3" {
		t.Errorf("expected the record to be kept, got %+v", record)
	}
}

func TestClusterStoreMarkInstalledWithoutRecord(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})

	err := store.markInstalled(managementCluster("mgmt"))

	checkError(t, err, "management cluster mgmt has no record")
}

func TestClusterStoreDestroyingWithoutRecord(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
	c := managementCluster("mgmt")
	writeTestFile(t, filepath.Join(store.installDir(c), "install-config.yaml"))

	_, found, err := store.destroying(c)

	if err != nil || found {
		t.Errorf("expected nothing to destroy, got found=%v err=%v", found, err)
	}
	if _, err := os.Stat(store.dir(c)); !os.IsNotExist(err) {
		t.Errorf("expected the directory to be removed, got %v", err)
	}
}

func TestClusterStoreForget(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
	mgmt := managementCluster("mgmt")
	hosted := hostedCluster("mgmt", "workload")
	for _, c := range []clusterRef{mgmt, hosted} {
		if err := store.begin(c, clusterRecord{Platform: platformAzure, InfraID: "workload-abcde", Azure: &azureRecord{ResourceGroup: "shared"}}); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.forget(hosted); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.load(hosted); found {
		t.Error("expected the hosted cluster to be forgotten")
	}
	if _, found, _ := store.load(mgmt); !found {
		t.Error("expected the management cluster to be kept")
	}
}

func TestClusterStoreList(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
	aws := managementCluster("b-mgmt")
	for _, c := range []clusterRef{aws, hostedCluster("b-mgmt", "b-workload-2"), hostedCluster("b-mgmt", "b-workload")} {
		if err := store.begin(c, clusterRecord{Platform: platformAWS, PullSecret: "pull-secret.json", InfraID: "infra"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.begin(managementCluster("c-aks"), clusterRecord{Platform: platformAzure}); err != nil {
		t.Fatal(err)
	}
	// Creating a-new never started.
	if err := store.track(managementCluster("a-new")); err != nil {
		t.Fatal(err)
	}

	clusters, err := store.list()
	if err != nil {
		t.Fatal(err)
	}

	type entry struct {
		ref      clusterRef
		platform string
		recorded bool
	}
	var got []entry
	for _, c := range clusters {
		got = append(got, entry{c.clusterRef, c.record.Platform, c.recorded})
	}
	want := []entry{
		{managementCluster("a-new"), "", false},
		{aws, platformAWS, true},
		{hostedCluster("b-mgmt", "b-workload"), platformAWS, true},
		{hostedCluster("b-mgmt", "b-workload-2"), platformAWS, true},
		{managementCluster("c-aks"), platformAzure, true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("clusters:\ngot:  %+v\nwant: %+v", got, want)
	}
	if c, found := clusters.find("b-workload"); !found || c.clusterRef != hostedCluster("b-mgmt", "b-workload") {
		t.Errorf("find b-workload: got %+v, %v", c, found)
	}
	if _, found := clusters.find("missing"); found {
		t.Error("expected missing not to be found")
	}
	if got, want := clusters.hosted("b-mgmt").names(), []string{"b-workload", "b-workload-2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("hosted clusters of b-mgmt: got %v, want %v", got, want)
	}
}

func TestClusterStoreListWithoutClusters(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})

	clusters, err := store.list()

	if err != nil || len(clusters) != 0 {
		t.Errorf("expected no clusters, got %v, %v", clusters, err)
	}
}

// The fixtures in testdata/data were written by hyperctl before the cluster
// record module, existing clusters must keep working.

func TestClusterStoreLoadsExistingRecords(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: copyFixtures(t)})
	shared := &azureRecord{ResourceGroup: "tester-hyperctl", DNSZone: "tester-hyperctl.azure.example.com", BaseDomainResourceGroup: "os4-common"}
	tests := map[string]struct {
		cluster clusterRef
		want    clusterRecord
	}{
		"OpenShift management cluster": {
			cluster: managementCluster("aws-mgmt"),
			want: clusterRecord{Platform: platformAWS, Phase: phaseInstalled, Release: "5.1.0-ec.0", Region: "eu-central-1",
				BaseDomain: "example.com", PullSecret: "/home/tester/.openshift/pull-secret-latest.json"},
		},
		"AWS hosted cluster": {
			cluster: hostedCluster("aws-mgmt", "aws-mgmt-workload"),
			want: clusterRecord{Platform: platformAWS, Phase: phaseInstalling, Release: "5.1.0-ec.0", Region: "eu-central-1",
				BaseDomain: "example.com", InfraID: "aws-mgmt-workload-abcde"},
		},
		"AKS management cluster": {
			cluster: managementCluster("aks-mgmt"),
			want: clusterRecord{Platform: platformAzure, Phase: phaseInstalled, Release: "5.1.0-ec.0", Region: "westus3",
				BaseDomain: "azure.example.com", PullSecret: "/home/tester/.openshift/pull-secret-latest.json"},
		},
		"Azure hosted cluster": {
			cluster: hostedCluster("aks-mgmt", "aks-mgmt-workload"),
			want: clusterRecord{Platform: platformAzure, Phase: phaseInstalled, Release: "5.1.0-ec.0", Region: "westus3",
				BaseDomain: "azure.example.com", InfraID: "aks-mgmt-workload-fghij", Azure: shared},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, found, err := store.destroying(tc.cluster)
			if err != nil || !found {
				t.Fatalf("expected a record, got found=%v err=%v", found, err)
			}
			if got.UpdatedAt.IsZero() {
				t.Error("expected the update time")
			}
			got.UpdatedAt = tc.want.UpdatedAt
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestClusterStoreForgetsExistingClusters(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: copyFixtures(t)})
	hosted := hostedCluster("aws-mgmt", "aws-mgmt-workload")

	if err := store.forget(hosted); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.load(hosted); found {
		t.Error("expected the hosted cluster to be forgotten")
	}
	if _, found, _ := store.load(managementCluster("aws-mgmt")); !found {
		t.Error("expected the management cluster to be kept")
	}
}

func TestClusterStoreWritesTheExistingFormat(t *testing.T) {
	store := newClusterStore(xdg.Dirs{Data: t.TempDir()})
	c := hostedCluster("aks-mgmt", "aks-mgmt-workload")
	if err := store.begin(c, clusterRecord{Platform: platformAzure, Release: "5.1.0-ec.0", Region: "westus3",
		BaseDomain: "azure.example.com", InfraID: "aks-mgmt-workload-fghij",
		Azure: &azureRecord{ResourceGroup: "tester-hyperctl", DNSZone: "tester-hyperctl.azure.example.com", BaseDomainResourceGroup: "os4-common"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.markInstalled(c); err != nil {
		t.Fatal(err)
	}

	got := readRecordFields(t, filepath.Join(store.dir(c), "cluster.yaml"))
	want := readRecordFields(t, filepath.Join("testdata", "data", "clusters", "aks-mgmt", "hosted", "aks-mgmt-workload", "cluster.yaml"))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("record file:\ngot:  %v\nwant: %v", got, want)
	}
}

// copyFixtures copies the records in testdata/data to a data directory.
func copyFixtures(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	if err := os.CopyFS(data, os.DirFS(filepath.Join("testdata", "data"))); err != nil {
		t.Fatal(err)
	}
	return data
}

// readRecordFields reads a record file without its update time.
func readRecordFields(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := yaml.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "updatedAt")
	return fields
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := writeFile(path, []byte("test\n")); err != nil {
		t.Fatal(err)
	}
}

func checkError(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Errorf("unexpected error: %v", err)
	case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
		t.Errorf("expected error containing %q, got %v", want, err)
	}
}

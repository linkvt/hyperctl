package app_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/linkvt/hyperctl/internal/app"
)

const (
	testAzureLocation   = "germanywestcentral"
	testAzureBaseDomain = "azure.autoscaling-dev.devcluster.openshift.com"
	testAzureDNSZone    = "tester-hyperctl." + testAzureBaseDomain

	roleContributor = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	roleReader      = "acdd72a7-3385-48ef-bd42-f606fba81ae7"
	roleUAA         = "18d7d88d-d35e-4fb5-a5c3-7773c20a72d9"
	roleIngress     = "0336e1d3-7a87-462b-b6db-342b63f7802c"
)

var (
	testSubscriptionScope = "/subscriptions/" + testSubscriptionID
	testDNSZoneScope      = testSubscriptionScope + "/resourceGroups/tester-hyperctl/providers/Microsoft.Network/dnszones/" + testAzureDNSZone
	testBaseZoneScope     = testSubscriptionScope + "/resourceGroups/os4-common/providers/Microsoft.Network/dnszones/" + testAzureBaseDomain
)

func TestCreateAzureCreatesSharedResources(t *testing.T) {
	env := newTestEnv(t)

	env.createAzureClusters(t)

	if got := env.azure.resourceGroups["tester-hyperctl"]; got != testAzureLocation {
		t.Errorf("shared resource group location: got %q, want %q", got, testAzureLocation)
	}
	dataDir := filepath.Join(env.home, ".local/share/hyperctl/azure")
	var credentials map[string]string
	readJSON(t, filepath.Join(dataDir, "credentials.json"), &credentials)
	wantCredentials := map[string]string{
		"subscriptionId": testSubscriptionID,
		"tenantId":       testTenantID,
		"clientId":       "app-tester-hyperctl",
		"clientSecret":   "secret-1",
	}
	if !maps.Equal(credentials, wantCredentials) {
		t.Errorf("credentials.json: got %v, want %v", credentials, wantCredentials)
	}
	var externalDNS map[string]string
	readJSON(t, filepath.Join(dataDir, "external-dns-credentials.json"), &externalDNS)
	wantExternalDNS := map[string]string{
		"tenantId":        testTenantID,
		"subscriptionId":  testSubscriptionID,
		"resourceGroup":   "tester-hyperctl",
		"aadClientId":     "app-tester-hyperctl-external-dns",
		"aadClientSecret": "secret-2",
	}
	if !maps.Equal(externalDNS, wantExternalDNS) {
		t.Errorf("external-dns-credentials.json: got %v, want %v", externalDNS, wantExternalDNS)
	}
	for _, want := range []fakeRoleAssignment{
		{testSubscriptionScope, roleContributor, "object-tester-hyperctl"},
		{testSubscriptionScope, roleUAA, "object-tester-hyperctl"},
		{testDNSZoneScope, roleReader, "object-tester-hyperctl-external-dns"},
		{testDNSZoneScope, roleContributor, "object-tester-hyperctl-external-dns"},
	} {
		if !env.azure.hasRoleAssignment(want.scope, want.role, want.principal) {
			t.Errorf("missing role assignment %+v", want)
		}
	}

	documents := env.azure.oidcIssuers["testerhyperctloidc"]
	var discovery map[string]any
	if err := json.Unmarshal(documents[".well-known/openid-configuration"], &discovery); err != nil {
		t.Fatalf("parsing discovery document: %v", err)
	}
	if want := "https://testerhyperctloidc.blob.core.windows.net/testerhyperctloidc"; discovery["issuer"] != want {
		t.Errorf("issuer: got %v, want %s", discovery["issuer"], want)
	}
	if len(documents["openid/v1/jwks"]) == 0 {
		t.Error("expected a JWKS document")
	}
	if info, err := os.Stat(filepath.Join(dataDir, "serviceaccount-signer.private")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("expected a private signing key, got %v, %v", info, err)
	}

	if !env.azure.dnsZones["tester-hyperctl/"+testAzureDNSZone] {
		t.Errorf("expected DNS zone %s, got %v", testAzureDNSZone, env.azure.dnsZones)
	}
	delegation := slices.IndexFunc(env.azure.dnsRecords["os4-common/"+testAzureBaseDomain], func(r app.AzureDNSRecord) bool {
		return r.Name == "tester-hyperctl" && r.Type == "NS"
	})
	if delegation < 0 {
		t.Errorf("expected NS record tester-hyperctl in the base domain zone, got %v", env.azure.dnsRecords)
	}
}

func TestCreateAzureCreatesAKSManagementCluster(t *testing.T) {
	env := newTestEnv(t)

	env.createAzureClusters(t)

	want := []app.AKSClusterParams{{
		ResourceGroup: "test-mgmt-rg",
		Name:          "test-mgmt",
		Location:      testAzureLocation,
		VMSize:        "Standard_D4s_v5",
		Nodes:         3,
		Zones:         []string{"1", "2"},
	}}
	if !slices.EqualFunc(env.azure.aksClusters, want, aksParamsEqual) {
		t.Errorf("AKS clusters:\ngot:  %+v\nwant: %+v", env.azure.aksClusters, want)
	}
	kubeconfig := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig")
	if content := readFile(t, kubeconfig); !strings.Contains(content, "test-mgmt.hcp.example.azmk8s.io") {
		t.Errorf("expected the AKS kubeconfig, got:\n%s", content)
	}
	if got := env.run(t, "kubeconfig", "mgmt"); strings.TrimSpace(got) != kubeconfig {
		t.Errorf("kubeconfig mgmt: got %q, want %q", got, kubeconfig)
	}
}

func TestCreateAzureInstallsHyperShiftOperatorWithExternalDNS(t *testing.T) {
	env := newTestEnv(t)

	env.createAzureClusters(t)

	if got := len(env.ocCalls(t, "apply --server-side -f https://")); got != 4 {
		t.Errorf("expected 4 CRD applies, got %d", got)
	}
	call := env.hypershiftCall(t, "install ")
	want := "install" +
		" --pull-secret " + env.pullSecret +
		" --limit-crd-install Azure" +
		" --tech-preview-no-upgrade" +
		" --enable-standalone-karpenter-operator" +
		" --external-dns-provider azure" +
		" --external-dns-credentials " + filepath.Join(env.home, ".local/share/hyperctl/azure/external-dns-credentials.json") +
		" --external-dns-domain-filter " + testAzureDNSZone +
		" --wait-until-available"
	if call.args != want {
		t.Errorf("hypershift args:\ngot:  %s\nwant: %s", call.args, want)
	}
	if want := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/install/auth/kubeconfig"); call.kubeconfig != want {
		t.Errorf("hypershift KUBECONFIG: got %q, want %q", call.kubeconfig, want)
	}
}

func TestCreateAzureCreatesHostedCluster(t *testing.T) {
	env := newTestEnv(t)

	env.createAzureClusters(t)

	infraID := env.hostedInfraID(t)
	hostedDir := filepath.Join(env.home, ".local/share/hyperctl/clusters/test-mgmt/hosted/test-mgmt-workload")
	credentials := filepath.Join(env.home, ".local/share/hyperctl/azure/credentials.json")
	issuer := "https://testerhyperctloidc.blob.core.windows.net/testerhyperctloidc"
	for prefix, want := range map[string]string{
		"create iam ": "create iam azure --name test-mgmt-workload --infra-id " + infraID +
			" --azure-creds " + credentials +
			" --location " + testAzureLocation +
			" --resource-group-name tester-hyperctl" +
			" --oidc-issuer-url " + issuer +
			" --output-file " + filepath.Join(hostedDir, "workload-identities.json") +
			" --enable-karpenter",
		"create infra ": "create infra azure --name test-mgmt-workload --infra-id " + infraID +
			" --azure-creds " + credentials +
			" --base-domain " + testAzureBaseDomain +
			" --location " + testAzureLocation +
			" --workload-identities-file " + filepath.Join(hostedDir, "workload-identities.json") +
			" --dns-zone-rg-name os4-common" +
			" --output-file " + filepath.Join(hostedDir, "infra-output.yaml"),
		"create cluster ": "create cluster azure --name test-mgmt-workload --infra-id " + infraID +
			" --azure-creds " + credentials +
			" --location " + testAzureLocation +
			" --base-domain " + testAzureBaseDomain +
			" --dns-zone-rg-name os4-common" +
			" --pull-secret " + env.pullSecret +
			" --release-image quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64" +
			" --node-pool-replicas 2" +
			" --oidc-issuer-url " + issuer +
			" --sa-token-issuer-private-key-path " + filepath.Join(env.home, ".local/share/hyperctl/azure/serviceaccount-signer.private") +
			" --infra-json " + filepath.Join(hostedDir, "infra-output.yaml") +
			" --diagnostics-storage-account-type Managed" +
			" --external-dns-domain " + testAzureDNSZone +
			" --annotations hypershift.openshift.io/pod-security-admission-label-override=baseline" +
			" --auto-node --wait --timeout 45m0s --generate-ssh",
	} {
		if got := env.hypershiftCall(t, prefix).args; got != want {
			t.Errorf("hypershift %s:\ngot:  %s\nwant: %s", prefix, got, want)
		}
	}
	if got := readFile(t, filepath.Join(hostedDir, "kubeconfig")); !strings.Contains(got, "api.hosted.fake") {
		t.Errorf("expected the hosted kubeconfig, got:\n%s", got)
	}
}

func TestCreateAzureAssignsWorkloadIdentityRoles(t *testing.T) {
	env := newTestEnv(t)

	env.createAzureClusters(t)

	infraID := env.hostedInfraID(t)
	var workloadAssignments int
	for _, a := range env.azure.roleAssignments {
		if strings.HasPrefix(a.principal, "principal-test-mgmt-workload-") {
			workloadAssignments++
		}
	}
	if workloadAssignments != 22 {
		t.Errorf("expected 22 workload identity role assignments, got %d: %+v", workloadAssignments, env.azure.roleAssignments)
	}
	clusterGroup := testSubscriptionScope + "/resourceGroups/test-mgmt-workload-" + infraID
	for _, want := range []fakeRoleAssignment{
		{testBaseZoneScope, roleIngress, "principal-test-mgmt-workload-ingress-" + infraID},
		{clusterGroup, "9980e02c-c2be-4d73-94e8-173b1dc7cf3c", "principal-test-mgmt-workload-karpenter-" + infraID},
		{testSubscriptionScope + "/resourceGroups/test-mgmt-workload-vnet-" + infraID, "4d97b98b-1d4f-4787-a291-c67834d212e7", "principal-test-mgmt-workload-karpenter-" + infraID},
		{testSubscriptionScope + "/resourceGroups/test-mgmt-workload-nsg-" + infraID, roleContributor, "principal-test-mgmt-workload-control-plane-operator-" + infraID},
	} {
		if !env.azure.hasRoleAssignment(want.scope, want.role, want.principal) {
			t.Errorf("missing role assignment %+v", want)
		}
	}
}

func TestCreateAzureReusesSharedResources(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	env.run(t, "create", "--release", "5.1.0-ec.0", "--pull-secret", env.pullSecret)

	if env.azure.secretsAdded != 2 {
		t.Errorf("expected no new client secrets, got %d secrets", env.azure.secretsAdded)
	}
	if len(env.azure.aksClusters) != 1 {
		t.Errorf("expected the AKS cluster to be reused, got %d creations", len(env.azure.aksClusters))
	}
	var iamCreates int
	for _, call := range env.hypershiftCalls(t) {
		if strings.HasPrefix(call.args, "create iam ") {
			iamCreates++
		}
	}
	if iamCreates != 1 {
		t.Errorf("expected the hosted cluster to be reused, got %d IAM creations", iamCreates)
	}
}

func TestCreateAzureRemembersLocation(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	var cfg struct {
		Azure struct {
			Location string `json:"location"`
		} `json:"azure"`
	}
	content := readFile(t, filepath.Join(env.home, ".config/hyperctl/config.yaml"))
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Azure.Location != testAzureLocation {
		t.Errorf("remembered location: got %q, want %q in config:\n%s", cfg.Azure.Location, testAzureLocation, content)
	}
	env.run(t, "destroy", "--yes")

	env.run(t, "create", "--platform", "azure", "--management-only", "--release", "5.1.0-ec.0", "--name", "second", "--pull-secret", env.pullSecret)

	if got := env.azure.aksClusters[len(env.azure.aksClusters)-1].Location; got != testAzureLocation {
		t.Errorf("location of the second cluster: got %q, want %q", got, testAzureLocation)
	}
}

// TestCreateAzureDefaultsLocation runs create without a location and expects
// the built-in default.
func TestCreateAzureDefaultsLocation(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create", "--platform", "azure", "--release", "5.1.0-ec.0", "--name", "test-mgmt", "--pull-secret", env.pullSecret)

	if got, want := env.azure.aksClusters[0].Location, "westus3"; got != want {
		t.Errorf("location of the cluster: got %q, want %q", got, want)
	}
	var cfg struct {
		Azure struct {
			Location string `json:"location"`
		} `json:"azure"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, filepath.Join(env.home, ".config/hyperctl/config.yaml"))), &cfg); err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Azure.Location, "westus3"; got != want {
		t.Errorf("saved location: got %q, want %q", got, want)
	}
}

// TestCreateDoesNotRememberPlatform expects a later create to ask for the
// platform again instead of reusing the one of a destroyed cluster.
func TestCreateDoesNotRememberPlatform(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	stateContent := readFile(t, filepath.Join(env.home, ".local/state/hyperctl/state.yaml"))
	if strings.Contains(stateContent, "platform:") {
		t.Errorf("expected no remembered platform, got:\n%s", stateContent)
	}
	env.run(t, "destroy", "--yes")

	_, err := env.runErr(t, "create", "--release", "5.1.0-ec.0", "--name", "test-mgmt", "--pull-secret", env.pullSecret)

	// Azure fills in its default location, so only the AWS path demands a region.
	if err == nil || !strings.Contains(err.Error(), "--region") {
		t.Errorf("expected the AWS path to demand a region, got %v", err)
	}
	if len(env.azure.aksClusters) != 1 {
		t.Errorf("expected no second AKS cluster, got %v", env.azure.aksClusters)
	}
}

func TestCreateAzureUsesSharedResourcesFromConfig(t *testing.T) {
	env := newTestEnv(t)
	configPath := filepath.Join(env.home, ".config", "hyperctl", "config.yaml")
	mustMkdir(t, filepath.Dir(configPath))
	writeFile(t, configPath, `azure:
  location: germanywestcentral
  resourceGroup: vlink-autonode-persistent
  oidcStorageAccount: vlinkautonodeoidc
  dnsZone: vlink-mgmt.azure.autoscaling-dev.devcluster.openshift.com
`, 0o600)

	env.run(t, "create", "--platform", "azure", "--release", "5.1.0-ec.0", "--name", "test-mgmt", "--pull-secret", env.pullSecret)

	if _, found := env.azure.resourceGroups["vlink-autonode-persistent"]; !found {
		t.Errorf("expected the configured resource group, got %v", env.azure.resourceGroups)
	}
	if _, found := env.azure.oidcIssuers["vlinkautonodeoidc"]; !found {
		t.Errorf("expected the configured OIDC storage account, got %v", env.azure.oidcIssuers)
	}
	if !strings.Contains(env.hypershiftCall(t, "install ").args, "--external-dns-domain-filter vlink-mgmt.azure.autoscaling-dev.devcluster.openshift.com") {
		t.Errorf("expected the configured DNS zone, got %s", env.hypershiftCall(t, "install ").args)
	}
}

func TestCreateAzureRequiresStandaloneKarpenterOperator(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "create", "--platform", "azure", "--standalone-karpenter-operator=false",
		"--release", "5.1.0-ec.0", "--region", testAzureLocation, "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "standalone karpenter-operator") {
		t.Errorf("expected a standalone karpenter-operator error, got %v", err)
	}
}

func TestCreateAzureRequiresAvailableVMSize(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.runErr(t, "create", "--platform", "azure", "--release", "5.1.0-ec.0",
		"--region", "westus2", "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "not offered in location westus2") {
		t.Errorf("expected a VM size error, got %v", err)
	}
	if len(env.azure.resourceGroups) != 0 || len(env.azure.aksClusters) != 0 {
		t.Errorf("expected nothing created, got %v and %v", env.azure.resourceGroups, env.azure.aksClusters)
	}
}

func TestCreateRefusesOtherPlatformThanTrackedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	_, err := env.runErr(t, "create", "--platform", "aws", "--release", "5.1.0-ec.0", "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "runs on azure") {
		t.Errorf("expected a platform error, got %v", err)
	}
}

func TestDestroyAzureDestroysHostedAndManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	infraID := env.hostedInfraID(t)
	baseZone := "os4-common/" + testAzureBaseDomain
	env.azure.dnsRecords["tester-hyperctl/"+testAzureDNSZone] = []app.AzureDNSRecord{
		{Name: "@", Type: "NS"},
		{Name: "@", Type: "SOA"},
		{Name: "api-test-mgmt-workload", Type: "A"},
		{Name: "a-api-test-mgmt-workload-external-dns", Type: "TXT"},
	}
	env.azure.dnsRecords[baseZone] = append(env.azure.dnsRecords[baseZone],
		app.AzureDNSRecord{Name: "*.apps.test-mgmt-workload", Type: "A", Metadata: map[string]string{"kubernetes.io_cluster." + infraID: "owned"}},
		app.AzureDNSRecord{Name: "*.apps.test-mgmt-workload", Type: "AAAA", Metadata: map[string]string{"kubernetes.io_cluster.other-infra": "owned"}},
		app.AzureDNSRecord{Name: "*.apps.other", Type: "A", Metadata: map[string]string{"kubernetes.io_cluster." + infraID: "owned"}},
	)

	env.run(t, "destroy", "--yes")

	if got := env.ocCalls(t, "delete hostedcluster test-mgmt-workload -n clusters"); len(got) != 1 {
		t.Errorf("expected the HostedCluster to be deleted, got %v", got)
	}
	if got := env.ocCalls(t, "wait --for=delete namespace/clusters-test-mgmt-workload"); len(got) != 1 {
		t.Errorf("expected to wait for the control plane namespace, got %v", got)
	}
	var zoneRecords []string
	for _, r := range env.azure.dnsRecords["tester-hyperctl/"+testAzureDNSZone] {
		zoneRecords = append(zoneRecords, r.Type+" "+r.Name)
	}
	if want := []string{"NS @", "SOA @"}; !slices.Equal(zoneRecords, want) {
		t.Errorf("External DNS zone records: got %v, want %v", zoneRecords, want)
	}
	var baseRecords []string
	for _, r := range env.azure.dnsRecords[baseZone] {
		baseRecords = append(baseRecords, r.Type+" "+r.Name)
	}
	if want := []string{"NS tester-hyperctl", "AAAA *.apps.test-mgmt-workload", "A *.apps.other"}; !slices.Equal(baseRecords, want) {
		t.Errorf("base domain zone records: got %v, want %v", baseRecords, want)
	}
	if env.azure.hasRoleAssignment(testBaseZoneScope, roleIngress, "principal-test-mgmt-workload-ingress-"+infraID) {
		t.Error("expected the ingress role assignment on the base domain zone to be deleted")
	}
	wantGroups := []string{
		"test-mgmt-workload-" + infraID,
		"test-mgmt-workload-nsg-" + infraID,
		"test-mgmt-workload-vnet-" + infraID,
		"test-mgmt-rg",
		"MC_test-mgmt-rg_test-mgmt_" + testAzureLocation,
	}
	if !slices.Equal(env.azure.deletedResourceGroups, wantGroups) {
		t.Errorf("deleted resource groups:\ngot:  %v\nwant: %v", env.azure.deletedResourceGroups, wantGroups)
	}
	if len(env.azure.deletedIdentities) != 9 || !slices.Contains(env.azure.deletedIdentities, "tester-hyperctl/test-mgmt-workload-karpenter-"+infraID) {
		t.Errorf("expected the 9 workload identities to be deleted, got %v", env.azure.deletedIdentities)
	}
	if _, found := env.azure.resourceGroups["tester-hyperctl"]; !found {
		t.Error("expected the shared resource group to be kept")
	}
	if out := env.run(t, "status"); !strings.Contains(out, "No clusters") {
		t.Errorf("expected no clusters after destroy, got:\n%s", out)
	}
}

func TestDestroyAzureHostedWhenManagementClusterIsUnreachable(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	infraID := env.hostedInfraID(t)
	t.Setenv("HYPERCTL_TEST_API_DOWN", "1")

	env.run(t, "destroy", "hosted", "--yes")

	if got := env.ocCalls(t, "delete hostedcluster"); len(got) != 0 {
		t.Errorf("expected no HostedCluster deletion, got %v", got)
	}
	if !slices.Contains(env.azure.deletedResourceGroups, "test-mgmt-workload-"+infraID) {
		t.Errorf("expected the hosted cluster resource groups to be deleted, got %v", env.azure.deletedResourceGroups)
	}
	if slices.Contains(env.azure.deletedResourceGroups, "test-mgmt-rg") {
		t.Error("expected the management cluster to be kept")
	}
}

func TestDestroyAzureHostedWhenControlPlaneNamespaceIsAlreadyGone(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	infraID := env.hostedInfraID(t)
	t.Setenv("HYPERCTL_TEST_WAIT_ERROR", "error: no matching resources found")
	t.Setenv("HYPERCTL_TEST_NAMESPACE_GONE", "1")

	env.run(t, "destroy", "hosted", "--yes")

	if !slices.Contains(env.azure.deletedResourceGroups, "test-mgmt-workload-"+infraID) {
		t.Errorf("expected the hosted cluster resource groups to be deleted, got %v", env.azure.deletedResourceGroups)
	}
	if _, err := env.runErr(t, "kubeconfig", "hosted"); err == nil {
		t.Error("expected no hosted cluster after destroy")
	}
}

func TestDestroyAzureHostedFailsWhenControlPlaneNamespaceRemains(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	t.Setenv("HYPERCTL_TEST_WAIT_ERROR", "error: no matching resources found")

	_, err := env.runErr(t, "destroy", "hosted", "--yes")

	if err == nil || !strings.Contains(err.Error(), "waiting for namespace clusters-test-mgmt-workload to be deleted") {
		t.Errorf("expected a namespace wait error, got %v", err)
	}
	if len(env.azure.deletedResourceGroups) != 0 {
		t.Errorf("expected no Azure resources to be deleted, got %v", env.azure.deletedResourceGroups)
	}
}

func TestDestroySharedAzureResources(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	env.run(t, "destroy", "--yes")

	env.run(t, "destroy", "shared", "--yes")

	if want := []string{"app-tester-hyperctl", "app-tester-hyperctl-external-dns"}; !slices.Equal(env.azure.deletedApplications, want) {
		t.Errorf("deleted applications: got %v, want %v", env.azure.deletedApplications, want)
	}
	if env.azure.hasRoleAssignment(testSubscriptionScope, roleContributor, "object-tester-hyperctl") {
		t.Error("expected the subscription role assignments to be deleted")
	}
	if got := env.azure.dnsRecords["os4-common/"+testAzureBaseDomain]; len(got) != 0 {
		t.Errorf("expected the delegation to be deleted, got %v", got)
	}
	if !slices.Contains(env.azure.deletedResourceGroups, "tester-hyperctl") {
		t.Errorf("expected the shared resource group to be deleted, got %v", env.azure.deletedResourceGroups)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".local/share/hyperctl/azure")); !os.IsNotExist(err) {
		t.Errorf("expected the Azure data directory to be deleted, got %v", err)
	}
}

func TestDestroySharedRequiresDestroyedClusters(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	_, err := env.runErr(t, "destroy", "shared", "--yes")

	if err == nil || !strings.Contains(err.Error(), "destroy it first") {
		t.Errorf("expected an error about the management cluster, got %v", err)
	}
	if len(env.azure.deletedApplications) != 0 {
		t.Errorf("expected nothing deleted, got %v", env.azure.deletedApplications)
	}
}

// createAzureClusters creates an AKS management cluster and an Azure hosted
// cluster.
func (e *testEnv) createAzureClusters(t *testing.T) {
	t.Helper()
	e.run(t, "create",
		"--platform", "azure",
		"--release", "5.1.0-ec.0",
		"--region", testAzureLocation,
		"--name", "test-mgmt",
		"--pull-secret", e.pullSecret,
	)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(readFile(t, path)), v); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
}

func aksParamsEqual(a, b app.AKSClusterParams) bool {
	return a.ResourceGroup == b.ResourceGroup && a.Name == b.Name && a.Location == b.Location &&
		a.VMSize == b.VMSize && a.Nodes == b.Nodes && slices.Equal(a.Zones, b.Zones)
}

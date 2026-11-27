package app_test

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/linkvt/hyperctl/internal/azureaccount"
	"github.com/linkvt/hyperctl/internal/hypershift"
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
	delegation := slices.IndexFunc(env.azure.dnsRecords["os4-common/"+testAzureBaseDomain], func(r azureaccount.DNSRecord) bool {
		return r.Name == "tester-hyperctl" && r.Type == "NS"
	})
	if delegation < 0 {
		t.Errorf("expected NS record tester-hyperctl in the base domain zone, got %v", env.azure.dnsRecords)
	}
}

func TestCreateAzureCreatesAKSManagementCluster(t *testing.T) {
	env := newTestEnv(t)

	env.createAzureClusters(t)

	want := []azureaccount.AKSClusterParams{{
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
	paths := strings.Split(strings.TrimSpace(env.run(t, "kubeconfig")), string(os.PathListSeparator))
	if got := paths[len(paths)-1]; got != kubeconfig {
		t.Errorf("management cluster kubeconfig: got %q, want %q", got, kubeconfig)
	}
}

func TestCreateAzureInstallsHyperShiftOperatorWithExternalDNS(t *testing.T) {
	env := newTestEnv(t)

	env.createAzureClusters(t)

	if got := env.cluster.appliedURLs; len(got) != 4 {
		t.Errorf("expected 4 CRD applies, got %v", got)
	}
	call := single(t, env.hypershift.azureInstalls)
	want := hypershift.InstallAzureParams{
		PullSecret:                 env.pullSecret,
		ExternalDNSCredentialsFile: filepath.Join(env.home, ".local/share/hyperctl/azure/external-dns-credentials.json"),
		ExternalDNSDomain:          testAzureDNSZone,
	}
	if call.params != want {
		t.Errorf("hypershift install:\ngot:  %+v\nwant: %+v", call.params, want)
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
	wantIAM := hypershift.CreateAzureIAMParams{
		Name:            "test-mgmt-workload",
		InfraID:         infraID,
		CredentialsFile: credentials,
		Location:        testAzureLocation,
		ResourceGroup:   "tester-hyperctl",
		OIDCIssuerURL:   issuer,
		OutputFile:      filepath.Join(hostedDir, "workload-identities.json"),
	}
	if got := single(t, env.hypershift.azureIAMs).params; got != wantIAM {
		t.Errorf("hypershift create iam:\ngot:  %+v\nwant: %+v", got, wantIAM)
	}
	wantInfra := hypershift.CreateAzureInfraParams{
		Name:                   "test-mgmt-workload",
		InfraID:                infraID,
		CredentialsFile:        credentials,
		BaseDomain:             testAzureBaseDomain,
		Location:               testAzureLocation,
		WorkloadIdentitiesFile: filepath.Join(hostedDir, "workload-identities.json"),
		DNSZoneResourceGroup:   "os4-common",
		OutputFile:             filepath.Join(hostedDir, "infra-output.yaml"),
	}
	if got := single(t, env.hypershift.azureInfras).params; got != wantInfra {
		t.Errorf("hypershift create infra:\ngot:  %+v\nwant: %+v", got, wantInfra)
	}
	wantCluster := hypershift.CreateAzureClusterParams{
		Name:                 "test-mgmt-workload",
		InfraID:              infraID,
		CredentialsFile:      credentials,
		Location:             testAzureLocation,
		BaseDomain:           testAzureBaseDomain,
		DNSZoneResourceGroup: "os4-common",
		PullSecret:           env.pullSecret,
		ReleaseImage:         "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
		OIDCIssuerURL:        issuer,
		SigningKeyFile:       filepath.Join(env.home, ".local/share/hyperctl/azure/serviceaccount-signer.private"),
		InfraFile:            filepath.Join(hostedDir, "infra-output.yaml"),
		ExternalDNSDomain:    testAzureDNSZone,
	}
	if got := single(t, env.hypershift.azureClusters).params; got != wantCluster {
		t.Errorf("hypershift create cluster:\ngot:  %+v\nwant: %+v", got, wantCluster)
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

	env.run(t, "create", "--cluster", "test-mgmt", "--setup", "management", "--pull-secret", env.pullSecret)

	if env.azure.secretsAdded != 2 {
		t.Errorf("expected no new client secrets, got %d secrets", env.azure.secretsAdded)
	}
	if len(env.azure.aksClusters) != 1 {
		t.Errorf("expected the AKS cluster to be reused, got %d creations", len(env.azure.aksClusters))
	}
	if iamCreates := len(env.hypershift.azureIAMs); iamCreates != 1 {
		t.Errorf("expected the hosted cluster to be reused, got %d IAM creations", iamCreates)
	}
}

// TestCreateAzureSetupStandaloneSkipsHosting expects setup standalone on Azure to
// create only the AKS cluster, without the hypershift binary, the shared
// Azure resources, a release or a pull secret.
func TestCreateAzureSetupStandaloneSkipsHosting(t *testing.T) {
	env := newTestEnv(t)
	env.hypershift.path = ""

	env.run(t, "create", "--platform", "azure", "--setup", "standalone", "--region", testAzureLocation, "--name", "test-aks")

	if got := len(env.azure.aksClusters); got != 1 {
		t.Fatalf("expected the AKS cluster to be created, got %d", got)
	}
	if len(env.azure.applications) != 0 || len(env.azure.oidcIssuers) != 0 || len(env.azure.dnsZones) != 0 {
		t.Errorf("expected no shared Azure resources, got applications %v, OIDC issuers %d, DNS zones %v", env.azure.applications, len(env.azure.oidcIssuers), env.azure.dnsZones)
	}
	if got := len(env.images.pullChecks); got != 0 {
		t.Errorf("expected no release pull check, got %d", got)
	}
	if got := len(env.hypershift.azureInstalls); got != 0 {
		t.Errorf("expected no HyperShift operator, got %d installs", got)
	}
}

// TestCreateAzureAddsOperatorToCluster expects create with setup hosted on an
// AKS cluster without operator to add the operator and the hosted cluster,
// with the last used release instead of the one the cluster never had.
func TestCreateAzureAddsOperatorToCluster(t *testing.T) {
	env := newTestEnv(t)
	stateDir := filepath.Join(env.home, ".local/state/hyperctl")
	mustMkdir(t, stateDir)
	writeFile(t, filepath.Join(stateDir, "state.yaml"), "lastRelease: 5.1.0-ec.0\n", 0o600)
	env.run(t, "create", "--platform", "azure", "--setup", "standalone", "--region", testAzureLocation, "--name", "test-aks")
	record := readFile(t, filepath.Join(env.home, ".local/share/hyperctl/clusters/test-aks/cluster.yaml"))
	if !strings.Contains(record, `release: ""`) || strings.Contains(record, "pullSecret:") {
		t.Errorf("expected the cluster without operator to record no release or pull secret, got:\n%s", record)
	}

	env.run(t, "create", "--setup", "hosted", "--pull-secret", env.pullSecret)

	if got := len(env.hypershift.azureInstalls); got != 1 {
		t.Errorf("expected the HyperShift operator to be installed, got %d installs", got)
	}
	if got := single(t, env.hypershift.azureClusters).params.ReleaseImage; got != "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64" {
		t.Errorf("expected the hosted cluster with the last used release, got %s", got)
	}
	if len(env.azure.applications) == 0 {
		t.Error("expected the shared Azure resources to be created")
	}
	record = readFile(t, filepath.Join(env.home, ".local/share/hyperctl/clusters/test-aks/cluster.yaml"))
	if !strings.Contains(record, "hypershiftOperator: true") {
		t.Errorf("expected the record to note the HyperShift operator, got:\n%s", record)
	}
}

// TestCreateAzureKeepsValuesOfAddedOperator expects the record of an AKS
// cluster that got the operator later to hold the base domain and pull
// secret of the operator, so that the next run installs it with the same.
func TestCreateAzureKeepsValuesOfAddedOperator(t *testing.T) {
	env := newTestEnv(t)
	env.run(t, "create", "--platform", "azure", "--setup", "standalone", "--region", testAzureLocation, "--name", "test-aks")
	env.run(t, "create", "--setup", "management", "--pull-secret", env.pullSecret, "--base-domain", "other.example.com")

	env.run(t, "create", "--setup", "management")

	want := hypershift.InstallAzureParams{
		PullSecret:                 env.pullSecret,
		ExternalDNSCredentialsFile: filepath.Join(env.home, ".local/share/hyperctl/azure/external-dns-credentials.json"),
		ExternalDNSDomain:          "tester-hyperctl.other.example.com",
	}
	if got := env.hypershift.azureInstalls[len(env.hypershift.azureInstalls)-1].params; got != want {
		t.Errorf("expected the operator to be installed again with\n%+v\ngot\n%+v", want, got)
	}
}

func TestCreateAzureRemembersLocation(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	// The config holds what the user configured, the last used location is state.
	if _, err := os.Stat(filepath.Join(env.home, ".config/hyperctl/config.yaml")); !os.IsNotExist(err) {
		t.Errorf("expected no config file, got %v", err)
	}
	env.run(t, "destroy", "--yes")

	env.run(t, "create", "--platform", "azure", "--setup", "management", "--release", "5.1.0-ec.0", "--name", "second", "--pull-secret", env.pullSecret)

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
}

// TestCreateAzureConfiguredLocationWins expects the location from the config
// file to take precedence over the last used one.
func TestCreateAzureConfiguredLocationWins(t *testing.T) {
	env := newTestEnv(t)
	configPath := filepath.Join(env.home, ".config", "hyperctl", "config.yaml")
	mustMkdir(t, filepath.Dir(configPath))
	writeFile(t, configPath, "azure:\n  location: westus3\n", 0o600)
	env.run(t, "create", "--platform", "azure", "--setup", "management", "--release", "5.1.0-ec.0", "--region", testAzureLocation, "--name", "first", "--pull-secret", env.pullSecret)
	env.run(t, "destroy", "--yes")

	env.run(t, "create", "--platform", "azure", "--setup", "management", "--release", "5.1.0-ec.0", "--name", "second", "--pull-secret", env.pullSecret)

	if got, want := env.azure.aksClusters[1].Location, "westus3"; got != want {
		t.Errorf("location of the second cluster: got %q, want %q", got, want)
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
	if got := single(t, env.hypershift.azureInstalls).params.ExternalDNSDomain; got != "vlink-mgmt.azure.autoscaling-dev.devcluster.openshift.com" {
		t.Errorf("expected the configured DNS zone, got %s", got)
	}
}

// TestCreateAzureRequiresStandaloneKarpenterOperator expects the flag to be
// rejected before the Azure login is checked.
func TestCreateAzureRequiresStandaloneKarpenterOperator(t *testing.T) {
	env := newTestEnv(t)
	env.azure.subscriptionErr = errors.New("not logged in")

	_, err := env.runErr(t, "create", "--platform", "azure", "--standalone-karpenter-operator=false",
		"--release", "5.1.0-ec.0", "--region", testAzureLocation, "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "standalone karpenter-operator") {
		t.Errorf("expected a standalone karpenter-operator error, got %v", err)
	}
}

// TestCreateAzureRejectsOIDCBucket expects the AWS-only flag to be rejected
// before the Azure login is checked.
func TestCreateAzureRejectsOIDCBucket(t *testing.T) {
	env := newTestEnv(t)
	env.azure.subscriptionErr = errors.New("not logged in")

	_, err := env.runErr(t, "create", "--platform", "azure", "--oidc-bucket", "my-oidc",
		"--release", "5.1.0-ec.0", "--region", testAzureLocation, "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "--oidc-bucket does not apply to azure") {
		t.Errorf("expected the flag to be rejected, got %v", err)
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

func TestCreateAzureRefusesIncompleteManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.azure.createAKSClusterErr = errors.New("creating AKS cluster: quota exceeded")
	if _, err := env.runErr(t, env.azureCreateArgs()...); err == nil {
		t.Fatal("expected failing AKS creation to fail create")
	}
	env.azure.createAKSClusterErr = nil

	_, err := env.runErr(t, env.azureCreateArgs()...)

	if err == nil || !strings.Contains(err.Error(), "previous installation of management cluster test-mgmt did not complete") {
		t.Errorf("expected incomplete installation error, got %v", err)
	}
	if got := len(env.azure.aksClusters); got != 0 {
		t.Errorf("expected no AKS cluster, got %d", got)
	}
}

func TestCreateAzureRefusesIncompleteHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.hypershift.createClusterErr = errors.New("hypershift create cluster: exit status 1")
	if _, err := env.runErr(t, env.azureCreateArgs()...); err == nil {
		t.Fatal("expected failing hypershift to fail create")
	}
	env.hypershift.createClusterErr = nil

	_, err := env.runErr(t, "create", "--cluster", "test-mgmt", "--hosted-name", "test-mgmt-workload")

	if err == nil || !strings.Contains(err.Error(), "previous creation of hosted cluster test-mgmt-workload did not complete") {
		t.Errorf("expected incomplete creation error, got %v", err)
	}
	if got := len(env.hypershift.azureClusters); got != 1 {
		t.Errorf("expected 1 hosted cluster creation, got %d", got)
	}
}
func TestCreateRefusesOtherPlatformThanTrackedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	_, err := env.runErr(t, "create", "--platform", "aws", "--pull-secret", env.pullSecret)

	if err == nil || !strings.Contains(err.Error(), "runs on azure") {
		t.Errorf("expected a platform error, got %v", err)
	}
}

func TestDestroyAzureDestroysHostedAndManagementCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	infraID := env.hostedInfraID(t)
	baseZone := "os4-common/" + testAzureBaseDomain
	env.azure.dnsRecords["tester-hyperctl/"+testAzureDNSZone] = []azureaccount.DNSRecord{
		{Name: "@", Type: "NS"},
		{Name: "@", Type: "SOA"},
		{Name: "api-test-mgmt-workload", Type: "A"},
		{Name: "a-api-test-mgmt-workload-external-dns", Type: "TXT"},
	}
	env.azure.dnsRecords[baseZone] = append(env.azure.dnsRecords[baseZone],
		azureaccount.DNSRecord{Name: "*.apps.test-mgmt-workload", Type: "A", Metadata: map[string]string{"kubernetes.io_cluster." + infraID: "owned"}},
		azureaccount.DNSRecord{Name: "*.apps.test-mgmt-workload", Type: "AAAA", Metadata: map[string]string{"kubernetes.io_cluster.other-infra": "owned"}},
		azureaccount.DNSRecord{Name: "*.apps.other", Type: "A", Metadata: map[string]string{"kubernetes.io_cluster." + infraID: "owned"}},
	)

	env.run(t, "destroy", "--yes")

	if got, want := single(t, env.hypershift.azureDestroys).params, env.azureDestroyParams(t); got != want {
		t.Errorf("hypershift destroy cluster:\ngot:  %+v\nwant: %+v", got, want)
	}
	if got := env.hypershift.azureInfraDestroys; len(got) != 0 {
		t.Errorf("expected no hypershift destroy infra, got %+v", got)
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
	// The fake hypershift destroys the workload identities, so the ingress
	// role assignment is only found and deleted before destroy iam.
	if env.azure.hasRoleAssignment(testBaseZoneScope, roleIngress, "principal-test-mgmt-workload-ingress-"+infraID) {
		t.Error("expected the ingress role assignment on the base domain zone to be deleted")
	}
	wantGroups := []string{"test-mgmt-rg", "MC_test-mgmt-rg_test-mgmt_" + testAzureLocation}
	if !slices.Equal(env.azure.deletedResourceGroups, wantGroups) {
		t.Errorf("deleted resource groups:\ngot:  %v\nwant: %v", env.azure.deletedResourceGroups, wantGroups)
	}
	wantIAM := hypershift.DestroyAzureIAMParams{
		Name:                   "test-mgmt-workload",
		InfraID:                infraID,
		CredentialsFile:        filepath.Join(env.home, ".local/share/hyperctl/azure/credentials.json"),
		ResourceGroup:          "tester-hyperctl",
		WorkloadIdentitiesFile: filepath.Join(env.azureHostedDir(), "workload-identities.json"),
		DNSZoneResourceGroup:   "os4-common",
	}
	if got := single(t, env.hypershift.azureIAMDestroys).params; got != wantIAM {
		t.Errorf("hypershift destroy iam:\ngot:  %+v\nwant: %+v", got, wantIAM)
	}
	if _, found := env.azure.resourceGroups["tester-hyperctl"]; !found {
		t.Error("expected the shared resource group to be kept")
	}
	if out := env.run(t, "list"); !strings.Contains(out, "No clusters") {
		t.Errorf("expected no clusters after destroy, got:\n%s", out)
	}
}

func TestDestroyAzureKeepsExternalDNSRecordsOfOtherHostedClusters(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	zone := "tester-hyperctl/" + testAzureDNSZone
	env.azure.dnsRecords[zone] = []azureaccount.DNSRecord{
		{Name: "@", Type: "NS"},
		{Name: "api-test-mgmt-workload", Type: "A"},
		{Name: "api-test-mgmt-workload-external-dns", Type: "TXT"},
		{Name: "a-api-test-mgmt-workload-external-dns", Type: "TXT"},
		{Name: "oauth-test-mgmt-workload", Type: "CNAME"},
		{Name: "cname-oauth-test-mgmt-workload-external-dns", Type: "TXT"},
		{Name: "konnectivity-test-mgmt-workload", Type: "A"},
		{Name: "aaaa-konnectivity-test-mgmt-workload-external-dns", Type: "TXT"},
		{Name: "ignition-test-mgmt-workload", Type: "A"},
		{Name: "ignition-test-mgmt-workload-external-dns", Type: "TXT"},
		{Name: "api-other", Type: "A"},
		{Name: "a-api-other-external-dns", Type: "TXT"},
		{Name: "api-test-mgmt-workload-2", Type: "A"},
		{Name: "a-api-test-mgmt-workload-2-external-dns", Type: "TXT"},
		// The API host of a hosted cluster test-mgmt-workload-external-dns
		// has the name of a TXT record of test-mgmt-workload.
		{Name: "api-test-mgmt-workload-external-dns", Type: "A"},
	}

	env.run(t, "destroy", "--yes")

	var zoneRecords []string
	for _, r := range env.azure.dnsRecords[zone] {
		zoneRecords = append(zoneRecords, r.Type+" "+r.Name)
	}
	want := []string{
		"NS @",
		"A api-other",
		"TXT a-api-other-external-dns",
		"A api-test-mgmt-workload-2",
		"TXT a-api-test-mgmt-workload-2-external-dns",
		"A api-test-mgmt-workload-external-dns",
	}
	if !slices.Equal(zoneRecords, want) {
		t.Errorf("External DNS zone records:\ngot:  %v\nwant: %v", zoneRecords, want)
	}
}

func TestDestroyAzureHostedWhenManagementClusterIsUnreachable(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	env.cluster.apiDown = true

	env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	if got := env.hypershift.azureDestroys; len(got) != 0 {
		t.Errorf("expected no hypershift destroy cluster, got %+v", got)
	}
	if got, want := single(t, env.hypershift.azureInfraDestroys).params, env.azureDestroyParams(t); got != want {
		t.Errorf("hypershift destroy infra:\ngot:  %+v\nwant: %+v", got, want)
	}
	if got := env.hypershift.azureIAMDestroys; len(got) != 1 {
		t.Errorf("expected hypershift destroy iam, got %+v", got)
	}
	if slices.Contains(env.azure.deletedResourceGroups, "test-mgmt-rg") {
		t.Error("expected the management cluster to be kept")
	}
}

func TestDestroyAzureHostedDestroysHostedCluster(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)

	env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	if got := env.hypershift.azureDestroys; len(got) != 1 {
		t.Errorf("expected hypershift destroy cluster, got %+v", got)
	}
	if _, err := env.runErr(t, "kubeconfig", "--cluster", "test-mgmt-workload"); err == nil {
		t.Error("expected no hosted cluster after destroy")
	}
}

func TestDestroyAzureHostedStopsWhenDestroyClusterFails(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	env.hypershift.destroyAzureClusterErr = errors.New("hypershift destroy cluster: exit status 1")

	_, err := env.runErr(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	if err == nil || !strings.Contains(err.Error(), "hypershift destroy cluster") {
		t.Errorf("expected the hypershift error, got %v", err)
	}
	if got := env.hypershift.azureIAMDestroys; len(got) != 0 {
		t.Errorf("expected the workload identities to be kept, got %+v", got)
	}
	if out := env.run(t, "status"); !strings.Contains(out, "test-mgmt-workload") {
		t.Errorf("expected the hosted cluster to be kept, got:\n%s", out)
	}
}

func TestDestroyAzureHostedWithoutWorkloadIdentitiesFile(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	if err := os.Remove(filepath.Join(env.azureHostedDir(), "workload-identities.json")); err != nil {
		t.Fatal(err)
	}

	out := env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	if got := env.hypershift.azureIAMDestroys; len(got) != 0 {
		t.Errorf("expected no hypershift destroy iam, got %+v", got)
	}
	if !strings.Contains(out, "workload-identities.json") {
		t.Errorf("expected a warning about the missing workload identities file, got:\n%s", out)
	}
	if got := env.hypershift.azureDestroys; len(got) != 1 {
		t.Errorf("expected hypershift destroy cluster, got %+v", got)
	}
}

func TestDestroyAzureHostedWithoutClusterRecord(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	if err := os.Remove(filepath.Join(env.azureHostedDir(), "cluster.yaml")); err != nil {
		t.Fatal(err)
	}

	out := env.run(t, "destroy", "--cluster", "test-mgmt-workload", "--yes")

	if !strings.Contains(out, "Creation never started") {
		t.Errorf("expected nothing to destroy, got:\n%s", out)
	}
	if got := len(env.hypershift.azureDestroys) + len(env.hypershift.azureInfraDestroys) + len(env.hypershift.azureIAMDestroys); got != 0 {
		t.Errorf("expected no hypershift destroy, got %d", got)
	}
}
func TestDestroySharedAzureResources(t *testing.T) {
	env := newTestEnv(t)
	env.createAzureClusters(t)
	env.run(t, "destroy", "--yes")

	env.run(t, "destroy", "shared", "--platform", "azure", "--yes")

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

	_, err := env.runErr(t, "destroy", "shared", "--platform", "azure", "--yes")

	if err == nil || !strings.Contains(err.Error(), "clusters on azure exist, destroy them first: test-mgmt, test-mgmt-workload") {
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
	e.run(t, e.azureCreateArgs()...)
}

// azureCreateArgs create the clusters of createAzureClusters.
func (e *testEnv) azureCreateArgs() []string {
	return []string{"create",
		"--platform", "azure",
		"--release", "5.1.0-ec.0",
		"--region", testAzureLocation,
		"--name", "test-mgmt",
		"--pull-secret", e.pullSecret,
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(readFile(t, path)), v); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
}

func aksParamsEqual(a, b azureaccount.AKSClusterParams) bool {
	return a.ResourceGroup == b.ResourceGroup && a.Name == b.Name && a.Location == b.Location &&
		a.VMSize == b.VMSize && a.Nodes == b.Nodes && slices.Equal(a.Zones, b.Zones)
}

// azureHostedDir is the directory of the hosted cluster of createAzureClusters.
func (e *testEnv) azureHostedDir() string {
	return filepath.Join(e.home, ".local/share/hyperctl/clusters/test-mgmt/hosted/test-mgmt-workload")
}

// azureDestroyParams identify the hosted cluster of createAzureClusters.
func (e *testEnv) azureDestroyParams(t *testing.T) hypershift.DestroyAzureParams {
	t.Helper()
	return hypershift.DestroyAzureParams{
		Name:                 "test-mgmt-workload",
		InfraID:              e.hostedInfraID(t),
		CredentialsFile:      filepath.Join(e.home, ".local/share/hyperctl/azure/credentials.json"),
		Location:             testAzureLocation,
		DNSZoneResourceGroup: "os4-common",
	}
}

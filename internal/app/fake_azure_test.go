package app_test

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/linkvt/hyperctl/internal/azureaccount"
)

const (
	testSubscriptionID = "00000000-0000-0000-0000-000000000001"
	testTenantID       = "00000000-0000-0000-0000-000000000002"
)

// fakeAzure is an in-memory Azure subscription. Workload identities exist
// unless the fake hypershift destroyed them, as it does not create them.
type fakeAzure struct {
	// zones maps location/vmSize to the zones offering the VM size, other
	// VM sizes are not offered.
	zones map[string][]string
	// resourceGroups maps names to locations.
	resourceGroups        map[string]string
	deletedResourceGroups []string
	// applications maps app IDs to display names.
	applications        map[string]string
	deletedApplications []string
	secretsAdded        int
	roleAssignments     []fakeRoleAssignment
	destroyedIdentities []fakeWorkloadIdentities
	// oidcIssuers maps storage accounts to their documents.
	oidcIssuers map[string]map[string][]byte
	dnsZones    map[string]bool
	// dnsRecords maps resourceGroup/zone to its records.
	dnsRecords  map[string][]azureaccount.DNSRecord
	aksClusters []azureaccount.AKSClusterParams
	// createAKSClusterErr fails creating AKS clusters.
	createAKSClusterErr error
}

type fakeRoleAssignment struct {
	scope     string
	role      string
	principal string
}

// fakeWorkloadIdentities are the workload identities of a hosted cluster.
type fakeWorkloadIdentities struct {
	resourceGroup string
	cluster       string
	infraID       string
}

func newFakeAzure() *fakeAzure {
	return &fakeAzure{
		zones: map[string][]string{
			"germanywestcentral/Standard_D4s_v5": {"1", "2", "3"},
			"westus3/Standard_D4s_v5":            {"1", "2", "3"},
		},
		resourceGroups: map[string]string{},
		applications:   map[string]string{},
		oidcIssuers:    map[string]map[string][]byte{},
		dnsZones:       map[string]bool{},
		dnsRecords:     map[string][]azureaccount.DNSRecord{},
	}
}

func (f *fakeAzure) Subscription(context.Context) (azureaccount.Subscription, error) {
	return azureaccount.Subscription{ID: testSubscriptionID, Name: "Test Subscription", TenantID: testTenantID}, nil
}

func (f *fakeAzure) AvailableZones(_ context.Context, location, vmSize string) ([]string, error) {
	zones, found := f.zones[location+"/"+vmSize]
	if !found {
		return nil, fmt.Errorf("VM size %s is not offered in location %s", vmSize, location)
	}
	return zones, nil
}

func (f *fakeAzure) EnsureResourceGroup(_ context.Context, name, location string) (bool, error) {
	if _, found := f.resourceGroups[name]; found {
		return false, nil
	}
	f.resourceGroups[name] = location
	return true, nil
}

func (f *fakeAzure) DeleteResourceGroups(_ context.Context, names ...string) error {
	for _, name := range names {
		delete(f.resourceGroups, name)
		f.deletedResourceGroups = append(f.deletedResourceGroups, name)
	}
	return nil
}

func (f *fakeAzure) CreateServicePrincipal(_ context.Context, displayName string) (azureaccount.ServicePrincipal, error) {
	appID := "app-" + displayName
	f.applications[appID] = displayName
	f.secretsAdded++
	return azureaccount.ServicePrincipal{AppID: appID, ObjectID: "object-" + displayName, ClientSecret: fmt.Sprintf("secret-%d", f.secretsAdded)}, nil
}

func (f *fakeAzure) ServicePrincipalObjectID(_ context.Context, appID string) (string, bool, error) {
	displayName, found := f.applications[appID]
	if !found {
		return "", false, nil
	}
	return "object-" + displayName, true, nil
}

func (f *fakeAzure) DeleteApplication(_ context.Context, appID string) error {
	delete(f.applications, appID)
	f.deletedApplications = append(f.deletedApplications, appID)
	return nil
}

func (f *fakeAzure) EnsureRoleAssignment(_ context.Context, scope, role, principal string) error {
	assignment := fakeRoleAssignment{scope: scope, role: role, principal: principal}
	if !slices.Contains(f.roleAssignments, assignment) {
		f.roleAssignments = append(f.roleAssignments, assignment)
	}
	return nil
}

func (f *fakeAzure) DeleteRoleAssignments(_ context.Context, scope, principal string) error {
	f.roleAssignments = slices.DeleteFunc(f.roleAssignments, func(a fakeRoleAssignment) bool {
		return a.scope == scope && a.principal == principal
	})
	return nil
}

func (f *fakeAzure) IdentityPrincipalID(_ context.Context, resourceGroup, name string) (string, bool, error) {
	for _, d := range f.destroyedIdentities {
		if d.resourceGroup == resourceGroup && strings.HasPrefix(name, d.cluster+"-") && strings.HasSuffix(name, "-"+d.infraID) {
			return "", false, nil
		}
	}
	return "principal-" + name, true, nil
}

func (f *fakeAzure) EnsureOIDCIssuer(_ context.Context, resourceGroup, storageAccount, _ string, documents map[string][]byte) error {
	if _, found := f.resourceGroups[resourceGroup]; !found {
		return fmt.Errorf("resource group %s not found", resourceGroup)
	}
	f.oidcIssuers[storageAccount] = documents
	return nil
}

func (f *fakeAzure) EnsureDNSZone(_ context.Context, resourceGroup, zone string) ([]string, error) {
	f.dnsZones[resourceGroup+"/"+zone] = true
	return []string{"ns1.example.net.", "ns2.example.net."}, nil
}

func (f *fakeAzure) EnsureNSRecord(_ context.Context, resourceGroup, zone, name string, nameServers []string) error {
	key := resourceGroup + "/" + zone
	f.dnsRecords[key] = slices.DeleteFunc(f.dnsRecords[key], func(r azureaccount.DNSRecord) bool {
		return r.Name == name && r.Type == "NS"
	})
	f.dnsRecords[key] = append(f.dnsRecords[key], azureaccount.DNSRecord{Name: name, Type: "NS", Metadata: map[string]string{"ns": fmt.Sprint(nameServers)}})
	return nil
}

func (f *fakeAzure) DNSRecords(_ context.Context, resourceGroup, zone string) ([]azureaccount.DNSRecord, error) {
	return slices.Clone(f.dnsRecords[resourceGroup+"/"+zone]), nil
}

func (f *fakeAzure) DeleteDNSRecord(_ context.Context, resourceGroup, zone string, record azureaccount.DNSRecord) error {
	key := resourceGroup + "/" + zone
	f.dnsRecords[key] = slices.DeleteFunc(f.dnsRecords[key], func(r azureaccount.DNSRecord) bool {
		return r.Name == record.Name && r.Type == record.Type
	})
	return nil
}

func (f *fakeAzure) CreateAKSCluster(_ context.Context, params azureaccount.AKSClusterParams) error {
	if f.createAKSClusterErr != nil {
		return f.createAKSClusterErr
	}
	if _, found := f.resourceGroups[params.ResourceGroup]; !found {
		return fmt.Errorf("resource group %s not found", params.ResourceGroup)
	}
	f.aksClusters = append(f.aksClusters, params)
	return nil
}

func (f *fakeAzure) AKSAdminKubeconfig(_ context.Context, _, name string) ([]byte, error) {
	return []byte(`apiVersion: v1
kind: Config
clusters:
- name: ` + name + `
  cluster:
    server: https://` + name + `.hcp.example.azmk8s.io:443
contexts:
- name: ` + name + `-admin
  context:
    cluster: ` + name + `
    user: admin
current-context: ` + name + `-admin
users:
- name: admin
  user:
    token: aks-token
`), nil
}

// hasRoleAssignment reports whether a role is assigned to a principal at a
// scope.
func (f *fakeAzure) hasRoleAssignment(scope, role, principal string) bool {
	return slices.Contains(f.roleAssignments, fakeRoleAssignment{scope: scope, role: role, principal: principal})
}

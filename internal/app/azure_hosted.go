package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// Role definition IDs of the workload identities, see hypershift
// support/azureutil GetServicePrincipalScopes.
const (
	roleCloudProvider = "a1f96423-95ce-4224-ab27-4e3dc72facd4"
	roleIngress       = "0336e1d3-7a87-462b-b6db-342b63f7802c"
	roleFile          = "0d7aedc0-15fd-4a67-a412-efad370c947e"
	roleDisk          = "5b7237c5-45e1-49d6-bc18-a1f62f400748"
	roleNetwork       = "be7a6435-15ae-4171-8f30-4a343eff9e8f"
	roleImageRegistry = "8b32b316-c2f5-4ddf-b05b-83dacd2d08b5"
)

// ensureAzureHostedCluster creates the workload identities, infrastructure,
// role assignments and the hosted cluster with AutoNode and writes its
// kubeconfig, unless it is already created.
func ensureAzureHostedCluster(ctx context.Context, p progress, account AzureAccount, hs Hypershift, clusters clusterStore, opts createOptions, shared azureShared) error {
	c := hostedCluster(opts.Name, opts.HostedName)
	installed, err := clusters.check(c)
	if err != nil {
		return err
	}
	if installed {
		p.done("Hosted cluster %s is already created", opts.HostedName)
		return nil
	}

	// The infra ID and shared resources are recorded before creating
	// anything, so that the Azure resources can be destroyed even if
	// creating did not complete.
	record := clusterRecord{
		Platform:   platformAzure,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
		InfraID:    hypershift.NewInfraID(opts.HostedName),
		Azure: &azureRecord{
			ResourceGroup:           shared.ResourceGroup,
			DNSZone:                 shared.DNSZone,
			BaseDomainResourceGroup: shared.BaseDomainResourceGroup,
		},
	}
	if err := clusters.begin(c, record); err != nil {
		return err
	}
	dir := clusters.dir(c)
	identitiesFile := workloadIdentitiesFile(dir)
	infraFile := filepath.Join(dir, "infra-output.yaml")

	p.info("Creating workload identities")
	if err := hs.CreateAzureIAM(ctx, hypershift.CreateAzureIAMParams{
		Name:            opts.HostedName,
		InfraID:         record.InfraID,
		CredentialsFile: shared.CredentialsFile,
		Location:        opts.Region,
		ResourceGroup:   shared.ResourceGroup,
		OIDCIssuerURL:   shared.OIDCIssuerURL(),
		OutputFile:      identitiesFile,
	}); err != nil {
		return err
	}
	p.done("Created workload identities")
	p.info("Creating infrastructure")
	if err := hs.CreateAzureInfra(ctx, hypershift.CreateAzureInfraParams{
		Name:                   opts.HostedName,
		InfraID:                record.InfraID,
		CredentialsFile:        shared.CredentialsFile,
		BaseDomain:             opts.BaseDomain,
		Location:               opts.Region,
		WorkloadIdentitiesFile: identitiesFile,
		DNSZoneResourceGroup:   shared.BaseDomainResourceGroup,
		OutputFile:             infraFile,
	}); err != nil {
		return err
	}
	p.done("Created infrastructure")
	groups, err := readAzureInfraGroups(infraFile, shared.Subscription.ID)
	if err != nil {
		return err
	}
	assignments := workloadIdentityRoles(opts.HostedName, record.InfraID, groups, dnsZoneScope(shared.Subscription.ID, shared.BaseDomainResourceGroup, opts.BaseDomain))
	err = p.wait(ctx, "Assigning roles to the workload identities", func(ctx context.Context) error {
		return assignWorkloadIdentityRoles(ctx, account, shared.ResourceGroup, assignments)
	})
	if err != nil {
		return err
	}
	p.done("Assigned %d roles to the workload identities", len(assignments))

	p.info("Creating with AutoNode and waiting until it is available")
	start := time.Now()
	if err := hs.CreateAzureCluster(ctx, hypershift.CreateAzureClusterParams{
		Name:                 opts.HostedName,
		InfraID:              record.InfraID,
		CredentialsFile:      shared.CredentialsFile,
		Location:             opts.Region,
		BaseDomain:           opts.BaseDomain,
		DNSZoneResourceGroup: shared.BaseDomainResourceGroup,
		PullSecret:           opts.PullSecret,
		ReleaseImage:         ocp.ParseRelease(opts.Release).PullSpec,
		SSHKey:               opts.SSHKey,
		OIDCIssuerURL:        shared.OIDCIssuerURL(),
		SigningKeyFile:       shared.SigningKeyFile,
		InfraFile:            infraFile,
		ExternalDNSDomain:    shared.DNSZone,
	}); err != nil {
		return err
	}
	p.done("Created hosted cluster %s (%s)", opts.HostedName, since(start))

	kubeconfig, err := hs.HostedKubeconfig(ctx, opts.HostedName)
	if err != nil {
		return err
	}
	kubeconfigPath := clusters.kubeconfig(c)
	if err := writeFile(kubeconfigPath, kubeconfig); err != nil {
		return err
	}
	p.done("Wrote kubeconfig %s", kubeconfigPath)
	return clusters.markInstalled(c)
}

// workloadIdentitiesFile is the output of hypershift create iam azure in the
// directory of a hosted cluster.
func workloadIdentitiesFile(dir string) string {
	return filepath.Join(dir, "workload-identities.json")
}

// azureInfraGroups are the scopes of the resource groups of a hosted
// cluster's infrastructure.
type azureInfraGroups struct {
	cluster string
	nsg     string
	vnet    string
}

// readAzureInfraGroups reads the resource groups of the infrastructure from
// the output of hypershift create infra azure.
func readAzureInfraGroups(file, subscriptionID string) (azureInfraGroups, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return azureInfraGroups{}, err
	}
	var output struct {
		ResourceGroupName string `json:"resourceGroupName"`
		VNetID            string `json:"vnetID"`
		SecurityGroupID   string `json:"securityGroupID"`
	}
	if err := yaml.Unmarshal(data, &output); err != nil {
		return azureInfraGroups{}, fmt.Errorf("reading %s: %w", file, err)
	}
	if output.ResourceGroupName == "" {
		return azureInfraGroups{}, fmt.Errorf("reading %s: no resourceGroupName", file)
	}
	nsg, err := resourceGroupScope(output.SecurityGroupID)
	if err != nil {
		return azureInfraGroups{}, fmt.Errorf("reading %s: %w", file, err)
	}
	vnet, err := resourceGroupScope(output.VNetID)
	if err != nil {
		return azureInfraGroups{}, fmt.Errorf("reading %s: %w", file, err)
	}
	return azureInfraGroups{
		cluster: "/subscriptions/" + subscriptionID + "/resourceGroups/" + output.ResourceGroupName,
		nsg:     nsg,
		vnet:    vnet,
	}, nil
}

// resourceGroupScope returns the scope of the resource group of an Azure
// resource ID.
func resourceGroupScope(id string) (string, error) {
	scope, _, found := strings.Cut(id, "/providers/")
	if !found || !strings.Contains(scope, "/resourceGroups/") {
		return "", fmt.Errorf("%q is not the ID of a resource in a resource group", id)
	}
	return scope, nil
}

// roleAssignment assigns a role to a workload identity.
type roleAssignment struct {
	identity string
	role     string
	scope    string
}

// workloadIdentityRoles are the role assignments that hypershift create infra
// azure --assign-identity-roles would make. hyperctl makes them itself, as
// hypershift looks up the identities with Microsoft Graph, which the service
// principal may not read. The ingress identity gets its DNS role on the base
// domain zone instead of its whole resource group.
func workloadIdentityRoles(hosted, infraID string, groups azureInfraGroups, baseDomainZoneScope string) []roleAssignment {
	cluster, nsg, vnet := groups.cluster, groups.nsg, groups.vnet

	var assignments []roleAssignment
	for _, a := range []struct {
		component string
		role      string
		scopes    []string
	}{
		{"node-pool-mgmt", roleContributor, []string{cluster, vnet}},
		{"cloud-provider", roleCloudProvider, []string{cluster, nsg, vnet}},
		{"file", roleFile, []string{cluster, nsg, vnet}},
		{"disk", roleDisk, []string{cluster}},
		{"ingress", roleIngress, []string{cluster, vnet, baseDomainZoneScope}},
		{"network", roleNetwork, []string{cluster, vnet}},
		{"image-registry", roleImageRegistry, []string{cluster}},
		{"control-plane-operator", roleContributor, []string{cluster, nsg, vnet}},
		{"karpenter", roleVirtualMachineContributor, []string{cluster}},
		{"karpenter", roleNetworkContributor, []string{cluster, vnet}},
		{"karpenter", roleManagedIdentityOperator, []string{cluster}},
	} {
		for _, scope := range a.scopes {
			assignments = append(assignments, roleAssignment{identity: workloadIdentityName(hosted, a.component, infraID), role: a.role, scope: scope})
		}
	}
	return assignments
}

// assignWorkloadIdentityRoles makes the role assignments for the workload
// identities in resourceGroup.
func assignWorkloadIdentityRoles(ctx context.Context, account AzureAccount, resourceGroup string, assignments []roleAssignment) error {
	principals := map[string]string{}
	for _, a := range assignments {
		principalID, cached := principals[a.identity]
		if !cached {
			var found bool
			var err error
			principalID, found, err = account.IdentityPrincipalID(ctx, resourceGroup, a.identity)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("workload identity %s not found in resource group %s", a.identity, resourceGroup)
			}
			principals[a.identity] = principalID
		}
		if err := account.EnsureRoleAssignment(ctx, a.scope, a.role, principalID); err != nil {
			return fmt.Errorf("assigning role %s at %s to %s: %w", a.role, a.scope, a.identity, err)
		}
	}
	return nil
}

// workloadIdentityName is the name hypershift gives the workload identity of
// a component.
func workloadIdentityName(hosted, component, infraID string) string {
	return hosted + "-" + component + "-" + infraID
}

// destroyAzureHostedCluster destroys a hosted cluster with hypershift, only
// its infrastructure if the management cluster is unreachable, then the
// Azure resources that only hyperctl knows about and the workload
// identities.
func destroyAzureHostedCluster(ctx context.Context, p progress, deps Deps, dirs xdg.Dirs, clusters clusterStore, hs Hypershift, mgmt, hosted string) error {
	c := hostedCluster(mgmt, hosted)
	record, found, err := clusters.destroying(c)
	if err != nil {
		return err
	}
	if !found {
		p.done("Creation never started, nothing to destroy")
		return nil
	}
	kubeconfig := clusters.kubeconfig(managementCluster(mgmt))
	params := hypershift.DestroyAzureParams{
		Name:                 hosted,
		InfraID:              record.InfraID,
		CredentialsFile:      azureCredentialsFile(dirs),
		Location:             record.Region,
		DNSZoneResourceGroup: record.Azure.BaseDomainResourceGroup,
	}
	start := time.Now()
	var ready bool
	_ = p.wait(ctx, "Checking the management cluster API", func(ctx context.Context) error {
		ready = deps.Cluster.Ready(ctx, kubeconfig)
		return nil
	})
	if ready {
		p.info("Destroying")
		if err := hs.DestroyAzureCluster(ctx, params); err != nil {
			return err
		}
	} else {
		p.warn("Management cluster unreachable, destroying only the Azure resources of infra ID %s", record.InfraID)
		if err := hs.DestroyAzureInfra(ctx, params); err != nil {
			return err
		}
	}

	err = p.wait(ctx, "Deleting the DNS records and the ingress role assignment", func(ctx context.Context) error {
		subscription, err := deps.Azure.Subscription(ctx)
		if err != nil {
			return err
		}
		return deleteAzureHostedResources(ctx, deps.Azure, subscription.ID, hosted, record)
	})
	if err != nil {
		return err
	}

	identitiesFile := workloadIdentitiesFile(clusters.dir(c))
	switch _, err := os.Stat(identitiesFile); {
	case errors.Is(err, fs.ErrNotExist):
		p.warn("Skipping the workload identities, %s does not exist. Destroying the shared resources deletes any that were created.", identitiesFile)
	case err != nil:
		return err
	default:
		p.info("Destroying the workload identities")
		if err := hs.DestroyAzureIAM(ctx, hypershift.DestroyAzureIAMParams{
			Name:                   hosted,
			InfraID:                record.InfraID,
			CredentialsFile:        params.CredentialsFile,
			ResourceGroup:          record.Azure.ResourceGroup,
			WorkloadIdentitiesFile: identitiesFile,
			DNSZoneResourceGroup:   record.Azure.BaseDomainResourceGroup,
		}); err != nil {
			return err
		}
	}
	p.done("Destroyed hosted cluster %s (%s)", hosted, since(start))
	return clusters.forget(c)
}

// deleteAzureHostedResources deletes the Azure resources of a hosted cluster
// that hypershift does not know about: DNS records left by External DNS and
// the ingress operator, and the ingress role assignment on the base domain
// zone. The role assignment must be deleted before the workload identities.
func deleteAzureHostedResources(ctx context.Context, account AzureAccount, subscriptionID, hosted string, record clusterRecord) error {
	shared := record.Azure
	// External DNS only deletes its records after a while. The zone is
	// shared by all hosted clusters, so only the records of this one go.
	records, err := account.DNSRecords(ctx, shared.ResourceGroup, shared.DNSZone)
	if err != nil {
		return err
	}
	hosts, owners := externalDNSRecordNames(hosted)
	for _, r := range records {
		// A TXT record name of this cluster may be the host name of
		// another one, e.g. api-x-external-dns of hosted cluster
		// x-external-dns, so the type decides as well.
		names := hosts
		if r.Type == "TXT" {
			names = owners
		}
		if !slices.Contains(names, r.Name) {
			continue
		}
		if err := account.DeleteDNSRecord(ctx, shared.ResourceGroup, shared.DNSZone, r); err != nil {
			return err
		}
	}

	// The ingress operator of the hosted cluster writes its wildcard record
	// into the base domain zone, which other clusters share.
	records, err = account.DNSRecords(ctx, shared.BaseDomainResourceGroup, record.BaseDomain)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Name != "*.apps."+hosted || r.Metadata["kubernetes.io_cluster."+record.InfraID] != "owned" {
			continue
		}
		if err := account.DeleteDNSRecord(ctx, shared.BaseDomainResourceGroup, record.BaseDomain, r); err != nil {
			return err
		}
	}

	// Unlike the role assignments in the resource groups of the hosted
	// cluster, the one on the base domain zone outlives them.
	ingress, found, err := account.IdentityPrincipalID(ctx, shared.ResourceGroup, workloadIdentityName(hosted, "ingress", record.InfraID))
	if err != nil || !found {
		return err
	}
	return account.DeleteRoleAssignments(ctx, dnsZoneScope(subscriptionID, shared.BaseDomainResourceGroup, record.BaseDomain), ingress)
}

// externalDNSRecordNames returns the names of the External DNS records of a
// hosted cluster: the host names hypershift gives its services, and the
// names of the TXT records External DNS keeps to own them, which hypershift
// names with the suffix -external-dns and newer External DNS versions also
// with the record type as prefix.
func externalDNSRecordNames(hosted string) (hosts, owners []string) {
	for _, service := range []string{"api", "oauth", "konnectivity", "ignition"} {
		host := service + "-" + hosted
		hosts = append(hosts, host)
		owners = append(owners, host+"-external-dns")
		for _, recordType := range []string{"a", "aaaa", "cname"} {
			owners = append(owners, recordType+"-"+host+"-external-dns")
		}
	}
	return hosts, owners
}

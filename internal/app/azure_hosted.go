package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/oc"
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

// workloadIdentities are the components with a workload identity, as named
// by hypershift create iam azure --enable-karpenter.
var workloadIdentities = []string{
	"disk",
	"file",
	"image-registry",
	"ingress",
	"cloud-provider",
	"node-pool-mgmt",
	"network",
	"control-plane-operator",
	"karpenter",
}

// ensureAzureHostedCluster creates the workload identities, infrastructure,
// role assignments and the hosted cluster with AutoNode and writes its
// kubeconfig, unless it is already created.
func ensureAzureHostedCluster(ctx context.Context, p progress, account AzureAccount, hs hypershift.CLI, dirs xdg.Dirs, opts createOptions, shared azureShared) error {
	dir := hostedDir(dirs, opts.Name, opts.HostedName)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	switch {
	case found && record.Phase == phaseInstalled:
		p.done("Hosted cluster %s is already created", opts.HostedName)
		return nil
	case found:
		return fmt.Errorf("previous creation of hosted cluster %s did not complete, destroy it first", opts.HostedName)
	}

	// The infra ID and shared resources are recorded before creating
	// anything, so that the Azure resources can be destroyed even if
	// creating did not complete.
	record = clusterRecord{
		Platform:   platformAzure,
		Phase:      phaseInstalling,
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
	if err := saveClusterRecord(dir, record); err != nil {
		return err
	}
	identitiesFile := filepath.Join(dir, "workload-identities.json")
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
	assignments := workloadIdentityRoles(shared.Subscription.ID, opts.HostedName, record.InfraID, dnsZoneScope(shared.Subscription.ID, shared.BaseDomainResourceGroup, opts.BaseDomain))
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
	kubeconfigPath := hostedKubeconfigPath(dirs, opts.Name, opts.HostedName)
	if err := writeFile(kubeconfigPath, kubeconfig); err != nil {
		return err
	}
	p.done("Wrote kubeconfig %s", kubeconfigPath)
	record.Phase = phaseInstalled
	return saveClusterRecord(dir, record)
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
func workloadIdentityRoles(subscriptionID, hosted, infraID, baseDomainZoneScope string) []roleAssignment {
	resourceGroups := "/subscriptions/" + subscriptionID + "/resourceGroups/"
	cluster := resourceGroups + hosted + "-" + infraID
	nsg := resourceGroups + hosted + "-nsg-" + infraID
	vnet := resourceGroups + hosted + "-vnet-" + infraID

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

// destroyAzureHostedCluster deletes the HostedCluster, if the management
// cluster is reachable, and the Azure resources of the hosted cluster: its
// DNS records, resource groups, workload identities and their role
// assignments outside of the resource groups.
func destroyAzureHostedCluster(ctx context.Context, p progress, account AzureAccount, dirs xdg.Dirs, mgmt, hosted string) error {
	dir := hostedDir(dirs, mgmt, hosted)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	if !found {
		p.done("Creation never started, nothing to destroy")
		return os.RemoveAll(dir)
	}
	start := time.Now()
	kubeconfig := mgmtKubeconfigPath(dirs, mgmt)
	var ready bool
	_ = p.wait(ctx, "Checking the management cluster API", func(ctx context.Context) error {
		ready = oc.Ready(ctx, kubeconfig)
		return nil
	})
	if ready {
		err := p.wait(ctx, "Deleting the HostedCluster, this takes about 5 minutes", func(ctx context.Context) error {
			return oc.DeleteHostedCluster(ctx, kubeconfig, hosted, controlPlaneNamespace(hosted))
		})
		if err != nil {
			return err
		}
		p.done("Deleted the HostedCluster")
	} else {
		p.warn("Management cluster unreachable, deleting only the Azure resources of infra ID %s", record.InfraID)
	}

	var subscription AzureSubscription
	err = p.wait(ctx, "Deleting the Azure resources of infra ID "+record.InfraID, func(ctx context.Context) error {
		var err error
		if subscription, err = account.Subscription(ctx); err != nil {
			return err
		}
		return deleteAzureHostedResources(ctx, account, subscription.ID, hosted, record)
	})
	if err != nil {
		return err
	}
	p.done("Destroyed hosted cluster %s (%s)", hosted, since(start))
	return os.RemoveAll(dir)
}

// deleteAzureHostedResources deletes the Azure resources of a hosted cluster
// that deleting the HostedCluster leaves behind.
func deleteAzureHostedResources(ctx context.Context, account AzureAccount, subscriptionID, hosted string, record clusterRecord) error {
	shared := record.Azure
	// External DNS only deletes its records after a while. The zone holds
	// only the records of this hosted cluster, as there is only one.
	records, err := account.DNSRecords(ctx, shared.ResourceGroup, shared.DNSZone)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Name == "@" {
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

	// The role assignment on the base domain zone outlives the resource
	// groups and must be deleted before the identity.
	ingress, found, err := account.IdentityPrincipalID(ctx, shared.ResourceGroup, workloadIdentityName(hosted, "ingress", record.InfraID))
	if err != nil {
		return err
	}
	if found {
		if err := account.DeleteRoleAssignments(ctx, dnsZoneScope(subscriptionID, shared.BaseDomainResourceGroup, record.BaseDomain), ingress); err != nil {
			return err
		}
	}

	if err := account.DeleteResourceGroups(ctx,
		hosted+"-"+record.InfraID,
		hosted+"-nsg-"+record.InfraID,
		hosted+"-vnet-"+record.InfraID,
	); err != nil {
		return err
	}
	for _, component := range workloadIdentities {
		if err := account.DeleteIdentity(ctx, shared.ResourceGroup, workloadIdentityName(hosted, component, record.InfraID)); err != nil {
			return err
		}
	}
	return nil
}

package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/azureaccount"
	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/xdg"
)

const (
	defaultAzureBaseDomain = "azure.autoscaling-dev.devcluster.openshift.com"
	defaultAzureLocation   = "westus3"
)

// hypershiftCRDs are the CRDs the HyperShift operator needs that an AKS
// cluster lacks.
var hypershiftCRDs = []string{
	"https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml",
	"https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml",
	"https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_podmonitors.yaml",
	"https://raw.githubusercontent.com/openshift/api/6bababe9164ea6c78274fd79c94a3f951f8d5ab2/route/v1/zz_generated.crd-manifests/routes.crd.yaml",
}

// AzureAccount are the Azure operations hyperctl uses, with the credentials
// of the Azure CLI login. Deleting a missing resource is not an error.
type AzureAccount interface {
	// Subscription returns the default subscription of the Azure CLI login.
	Subscription(ctx context.Context) (azureaccount.Subscription, error)
	// AvailableZones returns the zones of a location that offer a VM size
	// without restrictions. It fails if the location does not offer it.
	AvailableZones(ctx context.Context, location, vmSize string) ([]string, error)

	// EnsureResourceGroup creates a resource group unless it exists and
	// reports whether it was created.
	EnsureResourceGroup(ctx context.Context, name, location string) (bool, error)
	// DeleteResourceGroups deletes resource groups and waits until they are
	// gone.
	DeleteResourceGroups(ctx context.Context, names ...string) error

	// CreateServicePrincipal returns the service principal of the
	// application with the display name, creating both if missing, and adds
	// a new client secret to the application.
	CreateServicePrincipal(ctx context.Context, displayName string) (azureaccount.ServicePrincipal, error)
	// ServicePrincipalObjectID returns the object ID of the service principal
	// of an application and whether it exists.
	ServicePrincipalObjectID(ctx context.Context, appID string) (string, bool, error)
	// DeleteApplication deletes an application and its service principal.
	DeleteApplication(ctx context.Context, appID string) error
	// EnsureRoleAssignment assigns a built-in role, given by its role
	// definition ID, to a service principal at a scope unless it is assigned.
	EnsureRoleAssignment(ctx context.Context, scope, roleDefinitionID, principalID string) error
	// DeleteRoleAssignments deletes the role assignments of a principal at
	// exactly the scope.
	DeleteRoleAssignments(ctx context.Context, scope, principalID string) error
	// IdentityPrincipalID returns the principal ID of a user-assigned managed
	// identity and whether it exists.
	IdentityPrincipalID(ctx context.Context, resourceGroup, name string) (string, bool, error)

	// EnsureOIDCIssuer creates a storage account with a publicly readable
	// container of the same name unless they exist, and uploads the
	// documents, keyed by their path in the container.
	EnsureOIDCIssuer(ctx context.Context, resourceGroup, storageAccount, location string, documents map[string][]byte) error

	// EnsureDNSZone creates a public DNS zone unless it exists and returns
	// its name servers.
	EnsureDNSZone(ctx context.Context, resourceGroup, zone string) ([]string, error)
	// EnsureNSRecord sets the name servers of an NS record set.
	EnsureNSRecord(ctx context.Context, resourceGroup, zone, name string, nameServers []string) error
	// DNSRecords lists the record sets of a zone.
	DNSRecords(ctx context.Context, resourceGroup, zone string) ([]azureaccount.DNSRecord, error)
	// DeleteDNSRecord deletes a record set.
	DeleteDNSRecord(ctx context.Context, resourceGroup, zone string, record azureaccount.DNSRecord) error

	// CreateAKSCluster creates an AKS cluster and waits until it is
	// provisioned.
	CreateAKSCluster(ctx context.Context, params azureaccount.AKSClusterParams) error
	// AKSAdminKubeconfig returns the admin kubeconfig of an AKS cluster.
	AKSAdminKubeconfig(ctx context.Context, resourceGroup, name string) ([]byte, error)
}

// azurePlatform creates AKS management clusters and Azure hosted clusters,
// together with the Azure resources they share with later clusters.
type azurePlatform struct {
	deps     Deps
	dirs     xdg.Dirs
	clusters clusterStore
}

// azureCreate is a create run on Azure.
type azureCreate struct {
	azurePlatform
	st    state
	cfg   config
	setup setup
	size  aksSize
	// subscription, zones and shared are found by checkAccount and prepare.
	subscription azureaccount.Subscription
	zones        []string
	shared       azureShared
}

func (a azurePlatform) newCreate(cmd *cli.Command, st state, cfg config, setup setup) (platformCreate, error) {
	if cmd.IsSet("oidc-bucket") {
		return nil, errors.New("--oidc-bucket does not apply to azure, it uses the OIDC issuer of the shared Azure resources")
	}
	if !setup.operator() {
		// AKS needs no release, pull secret or base domain.
		for _, name := range []string{"release", "base-domain", "pull-secret"} {
			if cmd.IsSet(name) {
				return nil, fmt.Errorf("--%s does not apply to setup %s on azure", name, setup)
			}
		}
	}
	if setup.operator() && !cmd.Bool("standalone-karpenter-operator") {
		return nil, errors.New("AutoNode on Azure requires the standalone karpenter-operator, remove --standalone-karpenter-operator=false")
	}
	run := &azureCreate{
		azurePlatform: a,
		st:            st,
		cfg:           cfg,
		setup:         setup,
		size: aksSize{
			VMSize: flagOr(cmd, "instance-type", cmp.Or(cfg.Azure.MgmtVMSize, defaultAzureVMSize)),
			Nodes:  cmp.Or(cfg.Azure.MgmtNodes, defaultAKSNodes),
		},
	}
	if cmd.IsSet("workers") {
		run.size.Nodes = cmd.Int("workers")
	}
	return run, nil
}

func (r *azureCreate) defaultRegion() string {
	return cmp.Or(r.cfg.Azure.Location, r.st.LastRegions.Azure, defaultAzureLocation)
}

func (r *azureCreate) defaultBaseDomain() string {
	return cmp.Or(r.cfg.Azure.BaseDomain, defaultAzureBaseDomain)
}

// needsRelease reports whether the HyperShift operator is installed, AKS
// needs no release, the operator needs the pull secret and the shared
// resources the base domain.
func (r *azureCreate) needsRelease() bool {
	return r.setup.operator()
}

func (r *azureCreate) regionPrompt() (string, string) {
	return "Azure location", "e.g. " + defaultAzureLocation + ", germanywestcentral, eastus"
}

func (r *azureCreate) promptSize(ctx context.Context, cmd *cli.Command) error {
	if cmd.IsSet("instance-type") || cmd.IsSet("workers") {
		return nil
	}
	return promptAKSSize(ctx, r.setup.clusterTitle()+" size", &r.size)
}

func (r *azureCreate) missing() []string {
	if r.size.VMSize == "" {
		return []string{"--instance-type"}
	}
	return nil
}

func (r *azureCreate) describeSize() string {
	return r.size.describe()
}

func (r *azureCreate) summary() []string {
	return nil
}

func (r *azureCreate) rememberRegion(st *state, region string) {
	st.LastRegions.Azure = region
}

func (r *azureCreate) checkAccount(ctx context.Context, p progress) error {
	err := p.wait(ctx, "Checking the Azure login", func(ctx context.Context) error {
		var err error
		r.subscription, err = r.deps.Azure.Subscription(ctx)
		return err
	})
	if err != nil {
		return err
	}
	p.done("Azure subscription %s (%s)", r.subscription.Name, r.subscription.ID)
	return nil
}

// prepare checks that the location offers the VM size of the AKS nodes and,
// for the HyperShift operator, creates the shared Azure resources that are
// missing.
func (r *azureCreate) prepare(ctx context.Context, p progress, opts createOptions) error {
	p.step("Checking the Azure subscription")
	var zones []string
	err := p.wait(ctx, "Checking "+r.size.VMSize+" in "+opts.Region, func(ctx context.Context) error {
		var err error
		zones, err = r.deps.Azure.AvailableZones(ctx, opts.Region, r.size.VMSize)
		return err
	})
	if err != nil {
		return err
	}
	r.zones = zones[:min(len(zones), aksZones)]
	p.done("%s is available in %s (zones: %s)", r.size.VMSize, opts.Region, strings.Join(r.zones, ", "))
	if !r.setup.operator() {
		return nil
	}

	p.step("Shared Azure resources")
	if r.shared, err = newAzureShared(r.dirs, r.cfg, r.subscription, opts.BaseDomain, opts.Region); err != nil {
		return err
	}
	return ensureAzureShared(ctx, p, r.deps.Azure, r.shared)
}

func (r *azureCreate) createMgmt(ctx context.Context, p progress, opts createOptions) error {
	return ensureAKSCluster(ctx, p, r.deps.Azure, r.clusters, opts, r.size, r.zones)
}

func (r *azureCreate) installOperator(ctx context.Context, p progress, hs Hypershift, opts createOptions) error {
	kubeconfig := r.clusters.kubeconfig(managementCluster(opts.Name))
	return installAzureOperator(ctx, p, r.deps.Cluster, hs, kubeconfig, opts, r.shared)
}

// installAzureOperator installs the HyperShift operator with External DNS,
// after adding the CRDs it expects from an OpenShift cluster.
func installAzureOperator(ctx context.Context, p progress, cluster ClusterAPI, hs Hypershift, kubeconfig string, opts createOptions, shared azureShared) error {
	err := p.wait(ctx, "Applying the Prometheus and Route CRDs", func(ctx context.Context) error {
		for _, url := range hypershiftCRDs {
			if err := cluster.ApplyServerSide(ctx, kubeconfig, url); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.done("Applied the Prometheus and Route CRDs")

	start := time.Now()
	if err := hs.InstallAzure(ctx, hypershift.InstallAzureParams{
		PullSecret:                 opts.PullSecret,
		ExternalDNSCredentialsFile: shared.ExternalDNSCredentialsFile,
		ExternalDNSDomain:          shared.DNSZone,
	}); err != nil {
		return err
	}
	p.done("Installed the HyperShift operator (%s)", since(start))
	return nil
}

func (r *azureCreate) createHosted(ctx context.Context, p progress, hs Hypershift, opts createOptions) error {
	return ensureAzureHostedCluster(ctx, p, r.deps.Azure, hs, r.clusters, opts, r.shared)
}

func (a azurePlatform) destroyHosted(ctx context.Context, p progress, hs Hypershift, _ bool, mgmt, hosted string) error {
	return destroyAzureHostedCluster(ctx, p, a.deps, a.dirs, a.clusters, hs, mgmt, hosted)
}

func (a azurePlatform) destroyMgmt(ctx context.Context, p progress, _ bool, name string) error {
	return destroyAKSCluster(ctx, p, a.deps.Azure, a.clusters, name)
}

// destroyShared destroys the service principals, the delegation of the
// External DNS zone and the shared resource group.
func (a azurePlatform) destroyShared(ctx context.Context, p progress, yes bool) error {
	cfg, err := loadConfig(a.dirs.Config)
	if err != nil {
		return err
	}
	var subscription azureaccount.Subscription
	err = p.wait(ctx, "Checking the Azure login", func(ctx context.Context) error {
		var err error
		subscription, err = a.deps.Azure.Subscription(ctx)
		return err
	})
	if err != nil {
		return err
	}
	shared, err := newAzureShared(a.dirs, cfg, subscription, cmp.Or(cfg.Azure.BaseDomain, defaultAzureBaseDomain), cfg.Azure.Location)
	if err != nil {
		return err
	}
	if !yes {
		if !a.deps.Interactive {
			return errors.New("destroying the shared Azure resources requires confirmation, pass --yes")
		}
		if err := confirmDestroy(ctx, []string{
			"service principals " + shared.ServicePrincipal + " and " + shared.ExternalDNSServicePrincipal,
			"NS record " + shared.delegationRecord() + " in " + shared.BaseDomain,
			"resource group " + shared.ResourceGroup + " with the OIDC issuer and DNS zone " + shared.DNSZone,
		}); err != nil {
			return err
		}
	}
	p.step("Shared Azure resources in subscription %s", subscription.Name)
	return destroyAzureShared(ctx, p, a.deps.Azure, a.dirs, shared)
}

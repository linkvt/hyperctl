package app

import (
	"context"
	"io"

	"github.com/urfave/cli/v3"
)

// AWSAccount are the AWS account operations hyperctl uses.
type AWSAccount interface {
	// CallerIdentity returns the ARN of the credentials in use.
	CallerIdentity(ctx context.Context) (string, error)
	// SpotRoleExists reports whether the EC2 Spot service-linked role exists.
	SpotRoleExists(ctx context.Context) (bool, error)
	// BucketRegion returns the region of a bucket and whether it exists.
	BucketRegion(ctx context.Context, bucket string) (string, bool, error)
	// CreatePublicBucket creates a bucket with publicly readable objects.
	CreatePublicBucket(ctx context.Context, bucket, region string) error
	// FirstZone returns the first available availability zone of a region.
	FirstZone(ctx context.Context, region string) (string, error)
	// ClusterDNS describes the DNS entries of a cluster with the given name.
	ClusterDNS(ctx context.Context, baseDomain, name string) ([]string, error)
	// DeleteClusterDNS deletes the DNS entries of a cluster with the given name.
	DeleteClusterDNS(ctx context.Context, baseDomain, name string) error
}

// AzureAccount are the Azure operations hyperctl uses, with the credentials
// of the Azure CLI login. Deleting a missing resource is not an error.
type AzureAccount interface {
	// Subscription returns the default subscription of the Azure CLI login.
	Subscription(ctx context.Context) (AzureSubscription, error)
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
	CreateServicePrincipal(ctx context.Context, displayName string) (AzureServicePrincipal, error)
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
	// DeleteIdentity deletes a user-assigned managed identity.
	DeleteIdentity(ctx context.Context, resourceGroup, name string) error

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
	DNSRecords(ctx context.Context, resourceGroup, zone string) ([]AzureDNSRecord, error)
	// DeleteDNSRecord deletes a record set.
	DeleteDNSRecord(ctx context.Context, resourceGroup, zone string, record AzureDNSRecord) error

	// CreateAKSCluster creates an AKS cluster and waits until it is
	// provisioned.
	CreateAKSCluster(ctx context.Context, params AKSClusterParams) error
	// AKSAdminKubeconfig returns the admin kubeconfig of an AKS cluster.
	AKSAdminKubeconfig(ctx context.Context, resourceGroup, name string) ([]byte, error)
}

// AzureSubscription identifies an Azure subscription.
type AzureSubscription struct {
	ID       string
	Name     string
	TenantID string
}

// AzureServicePrincipal is a service principal with a new client secret.
type AzureServicePrincipal struct {
	AppID        string
	ObjectID     string
	ClientSecret string
}

// AzureDNSRecord is a record set of an Azure DNS zone.
type AzureDNSRecord struct {
	// Name is relative to the zone, "@" is the apex.
	Name string
	// Type is the record type, e.g. "A" or "NS".
	Type     string
	Metadata map[string]string
}

// AKSClusterParams configure an AKS management cluster.
type AKSClusterParams struct {
	ResourceGroup string
	Name          string
	Location      string
	VMSize        string
	Nodes         int
	// Zones of the nodes, empty for a location without zones.
	Zones []string
}

// Deps are the external dependencies of the hyperctl CLI.
type Deps struct {
	AWS   AWSAccount
	Azure AzureAccount
	// DefaultRegion is the region of the active AWS profile, if any.
	DefaultRegion string
	// ReleasesURL is the base URL of the release controller.
	ReleasesURL string
	// Interactive enables prompts for values not given as flags and
	// spinners, it requires a terminal.
	Interactive bool
	Stdout      io.Writer
	Stderr      io.Writer
}

// New returns the hyperctl root command.
func New(deps Deps) *cli.Command {
	return &cli.Command{
		Name:      "hyperctl",
		Usage:     "Create a HyperShift management cluster and optional AutoNode hosted cluster",
		Writer:    deps.Stdout,
		ErrWriter: deps.Stderr,
		// Errors are returned to the caller instead of exiting the process.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Commands: []*cli.Command{
			cacheCommand(),
			createCommand(deps),
			destroyCommand(deps),
			imageCommand(),
			kubeconfigCommand(),
			releasesCommand(deps),
			statusCommand(deps),
		},
	}
}

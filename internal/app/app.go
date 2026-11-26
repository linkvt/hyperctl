package app

import (
	"context"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/azureaccount"
	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
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

// Hypershift are the hypershift CLI operations hyperctl uses, see the
// hypershift package.
type Hypershift interface {
	// Version checks that the binary runs.
	Version(ctx context.Context) error
	Install(ctx context.Context, params hypershift.InstallParams) error
	InstallAzure(ctx context.Context, params hypershift.InstallAzureParams) error
	CreateCluster(ctx context.Context, params hypershift.CreateClusterParams) error
	DestroyCluster(ctx context.Context, params hypershift.DestroyClusterParams) error
	DestroyInfra(ctx context.Context, params hypershift.DestroyClusterParams) error
	HostedKubeconfig(ctx context.Context, name string) ([]byte, error)
	CreateAzureIAM(ctx context.Context, params hypershift.CreateAzureIAMParams) error
	CreateAzureInfra(ctx context.Context, params hypershift.CreateAzureInfraParams) error
	CreateAzureCluster(ctx context.Context, params hypershift.CreateAzureClusterParams) error
	DestroyAzureCluster(ctx context.Context, params hypershift.DestroyAzureParams) error
	DestroyAzureInfra(ctx context.Context, params hypershift.DestroyAzureParams) error
	DestroyAzureIAM(ctx context.Context, params hypershift.DestroyAzureIAMParams) error
}

// ReleaseImages are the operations on OCP release images and the
// openshift-install binaries extracted from them that hyperctl uses, see the
// ocp package.
type ReleaseImages interface {
	CheckPullable(ctx context.Context, release ocp.Release, pullSecretPath string) error
	CachedInstaller(cacheDir string, release ocp.Release) (ocp.Installer, bool)
	ExtractInstaller(ctx context.Context, cacheDir string, release ocp.Release, pullSecretPath string) (ocp.Installer, error)
	CreateCluster(ctx context.Context, installer ocp.Installer, installDir string, release ocp.Release, out io.Writer) error
	DestroyCluster(ctx context.Context, installer ocp.Installer, installDir string, out io.Writer) error
}

// ClusterAPI are the queries and changes hyperctl runs against the
// management cluster of a kubeconfig, see the oc package.
type ClusterAPI interface {
	Ready(ctx context.Context, kubeconfig string) bool
	HostedClusterAvailable(ctx context.Context, kubeconfig, name string) (string, error)
	HostedKubeconfig(ctx context.Context, kubeconfig, name string) ([]byte, error)
	Annotations(ctx context.Context, kubeconfig, kind, namespace, name string) (map[string]string, error)
	Annotate(ctx context.Context, kubeconfig, kind, namespace, name, key, value string) error
	RemoveAnnotation(ctx context.Context, kubeconfig, kind, namespace, name, key string) error
	DeletePods(ctx context.Context, kubeconfig, namespace, selector string) error
	ApplyServerSide(ctx context.Context, kubeconfig, url string) error
}

// Deps are the external dependencies of the hyperctl CLI.
type Deps struct {
	AWS   AWSAccount
	Azure AzureAccount
	// Hypershift returns the hypershift binary at path, running against the
	// management cluster of kubeconfig and writing its output to out.
	Hypershift func(path, kubeconfig string, out io.Writer) Hypershift
	// LookPath finds a binary in PATH, see exec.LookPath.
	LookPath      func(file string) (string, error)
	ReleaseImages ReleaseImages
	Cluster       ClusterAPI
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
			imageCommand(deps),
			kubeconfigCommand(deps),
			releasesCommand(deps),
			statusCommand(deps),
		},
	}
}

package app

import (
	"context"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
)

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

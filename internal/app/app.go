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

// Deps are the external dependencies of the hyperctl CLI.
type Deps struct {
	AWS AWSAccount
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
		Usage:     "Create HyperShift management and hosted clusters with AutoNode",
		Writer:    deps.Stdout,
		ErrWriter: deps.Stderr,
		// Errors are returned to the caller instead of exiting the process.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Commands: []*cli.Command{
			cacheCommand(),
			createCommand(deps),
			destroyCommand(deps),
			kubeconfigCommand(),
			releasesCommand(deps),
			statusCommand(deps),
		},
	}
}

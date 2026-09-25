package app

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/urfave/cli/v3"
)

// STSClient is the subset of the AWS STS API hyperctl uses.
type STSClient interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// Deps are the external dependencies of the hyperctl CLI.
type Deps struct {
	STS STSClient
	// DefaultRegion is the region of the active AWS profile, if any.
	DefaultRegion string
	// Interactive enables prompts for values not given as flags.
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
			createCommand(deps),
			kubeconfigCommand(),
		},
	}
}

package app

import (
	"context"
	"fmt"

	"charm.land/huh/v2"
	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// Platforms of the clusters.
const (
	platformAWS   = "aws"
	platformAzure = "azure"
)

// platform creates and destroys clusters on one cloud platform: AWS with
// OpenShift management clusters (aws.go) or Azure with AKS management
// clusters (azure.go).
type platform interface {
	// newCreate starts a create run, reading the options of the platform from
	// the flags and the config file. It fails for set flags that don't apply
	// to the platform, before any prompt or cloud operation.
	// Each run skips the steps the setup does not need.
	newCreate(cmd *cli.Command, st state, cfg config, setup setup) (platformCreate, error)
	// destroyHosted destroys a hosted cluster with hs, which runs against its
	// management cluster.
	destroyHosted(ctx context.Context, p progress, hs Hypershift, yes bool, mgmt, hosted string) error
	destroyMgmt(ctx context.Context, p progress, yes bool, name string) error
	// destroyShared destroys the resources that the clusters share. If there
	// is anything to destroy, it asks for confirmation unless yes is set.
	destroyShared(ctx context.Context, p progress, yes bool) error
}

// platformCreate is one create run on a platform. It holds the options of
// the platform and what earlier steps found out for later ones. The driver
// calls the steps in their declaration order.
type platformCreate interface {
	// defaultRegion and defaultBaseDomain are used unless given as flags.
	defaultRegion() string
	defaultBaseDomain() string
	// needsRelease reports whether the cluster needs a release, a pull
	// secret and a base domain.
	needsRelease() bool
	// regionPrompt is the title and description of the region prompt.
	regionPrompt() (title, description string)
	// promptSize asks for the size of the cluster that create installs first.
	promptSize(ctx context.Context, cmd *cli.Command) error
	// missing returns the flags of required platform options without value.
	missing() []string
	// describeSize describes the size of the cluster that create installs
	// first.
	describeSize() string
	// summary returns further lines of the confirmation summary.
	summary() []string
	// rememberRegion saves the region as the last used one of the platform.
	rememberRegion(st *state, region string)

	// checkAccount checks the login to the cloud account.
	checkAccount(ctx context.Context, p progress) error
	// prepare checks the account prerequisites and creates the resources
	// that clusters share.
	prepare(ctx context.Context, p progress, opts createOptions) error
	createMgmt(ctx context.Context, p progress, opts createOptions) error
	installOperator(ctx context.Context, p progress, hs Hypershift, opts createOptions) error
	createHosted(ctx context.Context, p progress, hs Hypershift, opts createOptions) error
}

// newPlatform returns the platform with the given name.
func newPlatform(name string, deps Deps, dirs xdg.Dirs, clusters clusterStore) (platform, error) {
	switch name {
	case platformAWS:
		return awsPlatform{deps: deps, dirs: dirs, clusters: clusters}, nil
	case platformAzure:
		return azurePlatform{deps: deps, dirs: dirs, clusters: clusters}, nil
	default:
		return nil, fmt.Errorf("unknown platform %q, expected %s or %s", name, platformAWS, platformAzure)
	}
}

// resolvePlatform returns the platform of new clusters from the flag, or
// asks for it if it may prompt, by default aws.
func resolvePlatform(ctx context.Context, cmd *cli.Command, deps Deps) (string, error) {
	if !cmd.IsSet("platform") {
		if prompts(cmd, deps) {
			return promptPlatform(ctx, platformAWS)
		}
		return platformAWS, nil
	}
	platform := cmd.String("platform")
	if platform != platformAWS && platform != platformAzure {
		return "", fmt.Errorf("unknown platform %q, expected %s or %s", platform, platformAWS, platformAzure)
	}
	return platform, nil
}

// promptPlatform asks for the cloud platform of the clusters.
func promptPlatform(ctx context.Context, platform string) (string, error) {
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Platform").
			Options(
				huh.NewOption("AWS: OpenShift management cluster", platformAWS),
				huh.NewOption("Azure: AKS management cluster", platformAzure),
			).
			Value(&platform),
	)).RunWithContext(ctx)
	return platform, err
}

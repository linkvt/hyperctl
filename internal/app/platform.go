package app

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
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
	// the flags and the config file.
	newCreate(cmd *cli.Command, st state, cfg config) (platformCreate, error)
	// destroyHosted destroys a hosted cluster with hs, which runs against its
	// management cluster.
	destroyHosted(ctx context.Context, p progress, hs Hypershift, yes bool, mgmt, hosted string) error
	destroyMgmt(ctx context.Context, p progress, yes bool, name string) error
}

// platformCreate is one create run on a platform. It holds the options of
// the platform and what earlier steps found out for later ones. The driver
// calls the steps in their declaration order.
type platformCreate interface {
	// defaultRegion and defaultBaseDomain are used unless given as flags.
	defaultRegion() string
	defaultBaseDomain() string
	// regionPrompt is the title and description of the region prompt.
	regionPrompt() (title, description string)
	// promptSize asks for the size of the management cluster.
	promptSize(ctx context.Context, cmd *cli.Command) error
	// missing returns the flags of required platform options without value.
	missing() []string
	// describeSize describes the size of the management cluster.
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

// resolvePlatform returns the platform of the tracked management cluster, or
// the one from the flag, or asks for it when interactive.
func resolvePlatform(ctx context.Context, p progress, cmd *cli.Command, deps Deps, clusters clusterStore, st state) (string, error) {
	platform := platformAWS
	if cmd.IsSet("platform") {
		platform = cmd.String("platform")
		if platform != platformAWS && platform != platformAzure {
			return "", fmt.Errorf("unknown platform %q, expected %s or %s", platform, platformAWS, platformAzure)
		}
	}

	if st.MgmtCluster != "" {
		record, found, err := clusters.load(managementCluster(st.MgmtCluster))
		if err != nil {
			return "", err
		}
		if found {
			if cmd.IsSet("platform") && platform != record.Platform {
				return "", fmt.Errorf("management cluster %s runs on %s, destroy it first", st.MgmtCluster, record.Platform)
			}
			p.done("Platform %s (management cluster %s)", record.Platform, st.MgmtCluster)
			return record.Platform, nil
		}
	}
	if deps.Interactive && !cmd.IsSet("platform") {
		return promptPlatform(ctx, platform)
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

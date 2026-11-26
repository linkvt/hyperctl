package app

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// resolvePlatform returns the platform of the tracked management cluster, or
// the one from the flag, or asks for it when interactive.
func resolvePlatform(ctx context.Context, p progress, cmd *cli.Command, deps Deps, dirs xdg.Dirs, st state) (string, error) {
	platform := platformAWS
	if cmd.IsSet("platform") {
		platform = cmd.String("platform")
		if platform != platformAWS && platform != platformAzure {
			return "", fmt.Errorf("unknown platform %q, expected %s or %s", platform, platformAWS, platformAzure)
		}
	}

	if st.MgmtCluster != "" {
		record, found, err := newClusterStore(dirs).load(managementCluster(st.MgmtCluster))
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

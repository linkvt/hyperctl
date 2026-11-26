package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

const (
	destroyAll    = "all"
	destroyHosted = "hosted"
	destroyShared = "shared"
)

func destroyCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:      "destroy",
		Usage:     "Destroy the hosted cluster and, unless only hosted is given, the management cluster, or the shared resources of a platform",
		ArgsUsage: "[all|hosted|shared]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Usage: "Do not ask anything, also not for confirmation"},
			&cli.StringFlag{Name: "hypershift", Usage: "Path to the hypershift binary (default: from PATH)"},
			&cli.StringFlag{Name: "platform", Usage: "Platform of the shared resources to destroy, aws or azure"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runDestroy(ctx, cmd, deps)
		},
	}
}

func runDestroy(ctx context.Context, cmd *cli.Command, deps Deps) error {
	scope := cmd.Args().First()
	if scope == "" {
		scope = destroyAll
	}
	if scope != destroyAll && scope != destroyHosted && scope != destroyShared {
		return fmt.Errorf("unknown scope %q, expected %s, %s or %s", scope, destroyAll, destroyHosted, destroyShared)
	}

	dirs, err := xdg.Resolve("hyperctl")
	if err != nil {
		return err
	}
	st, err := loadState(dirs.State)
	if err != nil {
		return err
	}
	if scope == destroyShared {
		return runDestroyShared(ctx, cmd, deps, dirs, st)
	}
	if cmd.IsSet("platform") {
		return errors.New("--platform only applies to destroy shared")
	}
	if st.MgmtCluster == "" {
		return errors.New("no clusters to destroy")
	}
	clusters := newClusterStore(dirs)
	platformName, err := trackedPlatform(clusters, st)
	if err != nil {
		return err
	}
	platform, err := newPlatform(platformName, deps, dirs, clusters)
	if err != nil {
		return err
	}
	if !cmd.Bool("yes") {
		if !deps.Interactive {
			return errors.New("destroying clusters requires confirmation, pass --yes")
		}
		if err := confirmDestroy(ctx, destroyTargets(st, scope)); err != nil {
			return err
		}
	}

	p := progress{w: cmd.Root().Writer, interactive: deps.Interactive}
	if st.HostedCluster != "" {
		p.step("Hosted cluster %s", st.HostedCluster)
		cfg, err := loadConfig(dirs.Config)
		if err != nil {
			return err
		}
		hypershiftPath, err := resolveHypershift(ctx, cmd, deps, dirs, cfg)
		if err != nil {
			return err
		}
		hs := deps.Hypershift(hypershiftPath, clusters.kubeconfig(managementCluster(st.MgmtCluster)), p.tool("hypershift"))
		if err := platform.destroyHosted(ctx, p, hs, cmd.Bool("yes"), st.MgmtCluster, st.HostedCluster); err != nil {
			return err
		}
		st.HostedCluster = ""
		if err := saveState(dirs.State, st); err != nil {
			return err
		}
	}
	if scope == destroyHosted {
		return nil
	}

	p.step("Management cluster %s", st.MgmtCluster)
	if err := platform.destroyMgmt(ctx, p, cmd.Bool("yes"), st.MgmtCluster); err != nil {
		return err
	}
	st.MgmtCluster = ""
	return saveState(dirs.State, st)
}

// runDestroyShared destroys the resources that the clusters of a platform
// share, which requires that no cluster of the platform is left.
func runDestroyShared(ctx context.Context, cmd *cli.Command, deps Deps, dirs xdg.Dirs, st state) error {
	platformName, err := sharedPlatform(ctx, cmd, deps)
	if err != nil {
		return err
	}
	clusters := newClusterStore(dirs)
	platform, err := newPlatform(platformName, deps, dirs, clusters)
	if err != nil {
		return err
	}
	if st.MgmtCluster != "" {
		tracked, err := trackedPlatform(clusters, st)
		if err != nil {
			return err
		}
		if tracked == platformName {
			return fmt.Errorf("management cluster %s exists, destroy it first", st.MgmtCluster)
		}
	}
	p := progress{w: cmd.Root().Writer, interactive: deps.Interactive}
	return platform.destroyShared(ctx, p, cmd.Bool("yes"))
}

// sharedPlatform returns the platform from the flag or asks for it if it
// may prompt.
func sharedPlatform(ctx context.Context, cmd *cli.Command, deps Deps) (string, error) {
	if cmd.IsSet("platform") {
		return cmd.String("platform"), nil
	}
	if !prompts(cmd, deps) {
		return "", errors.New("destroying shared resources requires --platform")
	}
	return promptPlatform(ctx, platformAWS)
}

// trackedPlatform returns the platform of the tracked management cluster.
// Only a cluster whose installation never started lacks a record, as create
// writes it before the first cloud resource, and destroying such a cluster
// is a no-op on either platform. AWS is returned then, as the default.
func trackedPlatform(clusters clusterStore, st state) (string, error) {
	record, found, err := clusters.load(managementCluster(st.MgmtCluster))
	if err != nil {
		return "", err
	}
	if found {
		return record.Platform, nil
	}
	return platformAWS, nil
}

// destroyTargets describes the clusters destroyed for the given scope.
func destroyTargets(st state, scope string) []string {
	var targets []string
	if st.HostedCluster != "" {
		targets = append(targets, "hosted cluster "+st.HostedCluster)
	}
	if scope == destroyAll {
		targets = append(targets, "management cluster "+st.MgmtCluster)
	}
	return targets
}

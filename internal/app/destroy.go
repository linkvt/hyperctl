package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

const destroyShared = "shared"

func destroyCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:      "destroy",
		Usage:     "Destroy the cluster, a management cluster with its hosted clusters, or with shared the shared resources of a platform",
		ArgsUsage: "[shared]",
		Flags: []cli.Flag{
			clusterFlag(),
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
	if cmd.Args().First() == destroyShared {
		if cmd.Args().Len() > 1 {
			return fmt.Errorf("unexpected argument %q after shared", cmd.Args().Get(1))
		}
		if cmd.IsSet("cluster") {
			return errors.New("--cluster does not apply to destroy shared")
		}
		return runDestroyShared(ctx, cmd, deps)
	}
	if err := noArguments(cmd); err != nil {
		return err
	}
	if cmd.IsSet("platform") {
		return errors.New("--platform only applies to destroy shared")
	}
	sel, err := selectCluster(cmd)
	if err != nil {
		return err
	}
	// A cluster whose creation never started has no record, destroying it
	// is a no-op on either platform.
	platform, err := newPlatform(cmp.Or(sel.management().record.Platform, platformAWS), deps, sel.dirs, sel.store)
	if err != nil {
		return err
	}
	targets := sel.hostedClusters()
	if !sel.cluster.isHosted() {
		targets = append(targets, sel.cluster)
	}
	if !cmd.Bool("yes") {
		if !deps.Interactive {
			return errors.New("destroying clusters requires confirmation, pass --yes")
		}
		var descriptions []string
		for _, c := range targets {
			descriptions = append(descriptions, c.String())
		}
		if err := confirmDestroy(ctx, descriptions); err != nil {
			return err
		}
	}

	p := progress{w: cmd.Root().Writer, interactive: deps.Interactive}
	var hs Hypershift
	for _, c := range targets {
		if c.isHosted() {
			p.step("Hosted cluster %s", c.hosted)
			if hs == nil {
				if hs, err = destroyHypershift(ctx, cmd, deps, sel, p); err != nil {
					return err
				}
			}
			err = platform.destroyHosted(ctx, p, hs, cmd.Bool("yes"), c.mgmt, c.hosted)
		} else {
			p.step("Management cluster %s", c.mgmt)
			err = platform.destroyMgmt(ctx, p, cmd.Bool("yes"), c.mgmt)
		}
		if err != nil {
			return err
		}
		if err := forgetCurrent(sel.dirs, c.clusterRef); err != nil {
			return err
		}
	}
	return nil
}

// destroyHypershift returns the hypershift binary running against the
// management cluster of the selection.
func destroyHypershift(ctx context.Context, cmd *cli.Command, deps Deps, sel selection, p progress) (Hypershift, error) {
	cfg, err := loadConfig(sel.dirs.Config)
	if err != nil {
		return nil, err
	}
	path, err := resolveHypershift(ctx, cmd, deps, sel.dirs, cfg)
	if err != nil {
		return nil, err
	}
	return deps.Hypershift(path, sel.store.kubeconfig(managementCluster(sel.cluster.mgmt)), p.tool("hypershift")), nil
}

// forgetCurrent moves the current cluster from a destroyed hosted cluster
// to its management cluster, and clears it for a destroyed management
// cluster.
func forgetCurrent(dirs xdg.Dirs, destroyed clusterRef) error {
	return updateState(dirs.State, func(st *state) {
		switch {
		case st.Current != destroyed.name():
		case destroyed.isHosted():
			st.Current = destroyed.mgmt
		default:
			st.Current = ""
		}
	})
}

// runDestroyShared destroys the resources that the clusters of a platform
// share, which requires that no cluster of the platform is left.
func runDestroyShared(ctx context.Context, cmd *cli.Command, deps Deps) error {
	dirs, err := xdg.Resolve("hyperctl")
	if err != nil {
		return err
	}
	platformName, err := sharedPlatform(ctx, cmd, deps)
	if err != nil {
		return err
	}
	clusters := newClusterStore(dirs)
	platform, err := newPlatform(platformName, deps, dirs, clusters)
	if err != nil {
		return err
	}
	list, err := clusters.list()
	if err != nil {
		return err
	}
	var remaining []string
	for _, c := range list {
		if c.record.Platform == platformName {
			remaining = append(remaining, c.name())
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("clusters on %s exist, destroy them first: %s", platformName, strings.Join(remaining, ", "))
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

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

const (
	destroyAll    = "all"
	destroyHosted = "hosted"
)

func destroyCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:      "destroy",
		Usage:     "Destroy the hosted cluster and, unless only hosted is given, the management cluster",
		ArgsUsage: "[all|hosted]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Usage: "Do not ask for confirmation"},
			&cli.StringFlag{Name: "hypershift", Usage: "Path to the hypershift binary (default: from PATH)"},
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
	if scope != destroyAll && scope != destroyHosted {
		return fmt.Errorf("unknown scope %q, expected %s or %s", scope, destroyAll, destroyHosted)
	}

	dirs, err := xdg.Resolve("hyperctl")
	if err != nil {
		return err
	}
	st, err := loadState(dirs.State)
	if err != nil {
		return err
	}
	if st.MgmtCluster == "" {
		return errors.New("no clusters to destroy")
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
		cfg, err := loadConfig(dirs.Config)
		if err != nil {
			return err
		}
		hypershiftPath, err := resolveHypershift(ctx, cmd, deps, dirs, cfg)
		if err != nil {
			return err
		}
		hs := hypershift.CLI{
			Path:       hypershiftPath,
			Kubeconfig: mgmtKubeconfigPath(dirs, st.MgmtCluster),
			Output:     p.tool("hypershift"),
		}
		p.step("Hosted cluster %s", st.HostedCluster)
		if err := destroyHostedCluster(ctx, p, cmd.Bool("yes"), deps, hs, dirs, st.MgmtCluster, st.HostedCluster); err != nil {
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
	if err := destroyMgmtCluster(ctx, p, cmd.Bool("yes"), deps, dirs, st.MgmtCluster); err != nil {
		return err
	}
	st.MgmtCluster = ""
	return saveState(dirs.State, st)
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

func destroyHostedCluster(ctx context.Context, p progress, yes bool, deps Deps, hs hypershift.CLI, dirs xdg.Dirs, mgmt, hosted string) error {
	dir := hostedDir(dirs, mgmt, hosted)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	if !found {
		p.done("Creation never started, nothing to destroy")
		return os.RemoveAll(dir)
	}
	p.info("Destroying")
	start := time.Now()
	if err := hs.DestroyCluster(ctx, hosted, record.Region, record.BaseDomain); err != nil {
		return err
	}
	p.done("Destroyed hosted cluster %s (%s)", hosted, since(start))
	if err := deleteDNSLeftovers(ctx, p, deps, yes, record.BaseDomain, hosted); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func destroyMgmtCluster(ctx context.Context, p progress, yes bool, deps Deps, dirs xdg.Dirs, name string) error {
	dir := clusterDir(dirs, name)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	if !found {
		p.done("Installation never started, nothing to destroy")
		return os.RemoveAll(dir)
	}
	installer, err := ensureInstaller(ctx, p, dirs, ocp.ParseRelease(record.Release), record.PullSecret)
	if err != nil {
		return err
	}
	p.info("Destroying")
	start := time.Now()
	if err := installer.DestroyCluster(ctx, installDir(dirs, name), p.tool("openshift-install")); err != nil {
		return err
	}
	p.done("Destroyed management cluster %s (%s)", name, since(start))
	if err := deleteDNSLeftovers(ctx, p, deps, yes, record.BaseDomain, name); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

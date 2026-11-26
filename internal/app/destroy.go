package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/azureaccount"
	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/oc"
	"github.com/linkvt/hyperctl/internal/ocp"
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
		Usage:     "Destroy the hosted cluster and, unless only hosted is given, the management cluster, or the shared Azure resources",
		ArgsUsage: "[all|hosted|shared]",
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
	if st.MgmtCluster == "" {
		return errors.New("no clusters to destroy")
	}
	platform, err := trackedPlatform(dirs, st)
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
		if platform == platformAzure {
			err = destroyAzureHostedCluster(ctx, p, deps.Azure, dirs, st.MgmtCluster, st.HostedCluster)
		} else {
			err = destroyAWSHostedCluster(ctx, p, cmd, deps, dirs, st.MgmtCluster, st.HostedCluster)
		}
		if err != nil {
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
	if platform == platformAzure {
		err = destroyAKSCluster(ctx, p, deps.Azure, dirs, st.MgmtCluster)
	} else {
		err = destroyMgmtCluster(ctx, p, cmd.Bool("yes"), deps, dirs, st.MgmtCluster)
	}
	if err != nil {
		return err
	}
	st.MgmtCluster = ""
	return saveState(dirs.State, st)
}

// runDestroyShared destroys the Azure resources shared by all clusters,
// which requires that no clusters are left.
func runDestroyShared(ctx context.Context, cmd *cli.Command, deps Deps, dirs xdg.Dirs, st state) error {
	if st.MgmtCluster != "" {
		return fmt.Errorf("management cluster %s exists, destroy it first", st.MgmtCluster)
	}
	cfg, err := loadConfig(dirs.Config)
	if err != nil {
		return err
	}
	p := progress{w: cmd.Root().Writer, interactive: deps.Interactive}
	var subscription azureaccount.Subscription
	err = p.wait(ctx, "Checking the Azure login", func(ctx context.Context) error {
		var err error
		subscription, err = deps.Azure.Subscription(ctx)
		return err
	})
	if err != nil {
		return err
	}
	shared, err := newAzureShared(dirs, cfg, subscription, cmp.Or(cfg.Azure.BaseDomain, defaultAzureBaseDomain), cfg.Azure.Location)
	if err != nil {
		return err
	}
	if !cmd.Bool("yes") {
		if !deps.Interactive {
			return errors.New("destroying the shared Azure resources requires confirmation, pass --yes")
		}
		if err := confirmDestroy(ctx, []string{
			"service principals " + shared.ServicePrincipal + " and " + shared.ExternalDNSServicePrincipal,
			"NS record " + shared.delegationRecord() + " in " + shared.BaseDomain,
			"resource group " + shared.ResourceGroup + " with the OIDC issuer and DNS zone " + shared.DNSZone,
		}); err != nil {
			return err
		}
	}
	p.step("Shared Azure resources in subscription %s", subscription.Name)
	return destroyAzureShared(ctx, p, deps.Azure, dirs, shared)
}

// trackedPlatform returns the platform of the tracked management cluster.
// Only a cluster whose installation never started lacks a record, as create
// writes it before the first cloud resource, and destroying such a cluster
// is a no-op on either platform. AWS is returned then, as the default.
func trackedPlatform(dirs xdg.Dirs, st state) (string, error) {
	record, found, err := loadClusterRecord(clusterDir(dirs, st.MgmtCluster))
	if err != nil {
		return "", err
	}
	if found {
		return record.Platform, nil
	}
	return platformAWS, nil
}

// destroyAWSHostedCluster destroys an AWS hosted cluster with the hypershift
// CLI.
func destroyAWSHostedCluster(ctx context.Context, p progress, cmd *cli.Command, deps Deps, dirs xdg.Dirs, mgmt, hosted string) error {
	cfg, err := loadConfig(dirs.Config)
	if err != nil {
		return err
	}
	hypershiftPath, err := resolveHypershift(ctx, cmd, deps, dirs, cfg)
	if err != nil {
		return err
	}
	hs := hypershift.New(hypershiftPath, mgmtKubeconfigPath(dirs, mgmt), p.tool("hypershift"))
	return destroyHostedCluster(ctx, p, cmd.Bool("yes"), deps, hs, dirs, mgmt, hosted)
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
	params := hypershift.DestroyClusterParams{
		Name:       hosted,
		InfraID:    record.InfraID,
		Region:     record.Region,
		BaseDomain: record.BaseDomain,
	}
	var ready bool
	_ = p.wait(ctx, "Checking the management cluster API", func(ctx context.Context) error {
		ready = oc.New().Ready(ctx, mgmtKubeconfigPath(dirs, mgmt))
		return nil
	})
	start := time.Now()
	switch {
	case ready:
		p.info("Destroying")
		if err := hs.DestroyCluster(ctx, params); err != nil {
			return err
		}
	case record.InfraID != "":
		p.info("Management cluster unreachable, destroying the AWS resources of infra ID %s", record.InfraID)
		if err := hs.DestroyInfra(ctx, params); err != nil {
			return err
		}
	default:
		return fmt.Errorf("management cluster unreachable and the infra ID of hosted cluster %s is unknown, destroy its AWS resources manually", hosted)
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
	if err := ocp.New().DestroyCluster(ctx, installer, installDir(dirs, name), p.tool("openshift-install")); err != nil {
		return err
	}
	p.done("Destroyed management cluster %s (%s)", name, since(start))
	if err := deleteDNSLeftovers(ctx, p, deps, yes, record.BaseDomain, name); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

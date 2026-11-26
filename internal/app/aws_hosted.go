package app

import (
	"context"
	"fmt"
	"time"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
)

// ensureHostedCluster creates the hosted cluster with AutoNode and writes its
// kubeconfig, unless it is already created.
func ensureHostedCluster(ctx context.Context, p progress, hs Hypershift, clusters clusterStore, opts createOptions) error {
	c := hostedCluster(opts.Name, opts.HostedName)
	installed, err := clusters.check(c)
	if err != nil {
		return err
	}
	if installed {
		p.done("Hosted cluster %s is already created", opts.HostedName)
		return nil
	}

	// The infra ID is chosen and recorded before creating the cluster, so
	// that its AWS resources can be destroyed without the management
	// cluster, even if creating did not complete.
	infraID := hypershift.NewInfraID(opts.HostedName)
	if err := clusters.begin(c, clusterRecord{
		Platform:   platformAWS,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
		InfraID:    infraID,
	}); err != nil {
		return err
	}
	p.info("Creating with AutoNode and waiting until it is available")
	start := time.Now()
	if err := hs.CreateCluster(ctx, hypershift.CreateClusterParams{
		Name:         opts.HostedName,
		InfraID:      infraID,
		Region:       opts.Region,
		BaseDomain:   opts.BaseDomain,
		PullSecret:   opts.PullSecret,
		ReleaseImage: ocp.ParseRelease(opts.Release).PullSpec,
		SSHKey:       opts.SSHKey,
	}); err != nil {
		return err
	}
	p.done("Created hosted cluster %s (%s)", opts.HostedName, since(start))

	kubeconfig, err := hs.HostedKubeconfig(ctx, opts.HostedName)
	if err != nil {
		return err
	}
	kubeconfigPath := clusters.kubeconfig(c)
	if err := writeFile(kubeconfigPath, kubeconfig); err != nil {
		return err
	}
	p.done("Wrote kubeconfig %s", kubeconfigPath)
	return clusters.markInstalled(c)
}

// destroyHostedCluster destroys an AWS hosted cluster with hypershift, only
// its AWS resources if the management cluster is unreachable.
func destroyHostedCluster(ctx context.Context, p progress, yes bool, deps Deps, hs Hypershift, clusters clusterStore, mgmt, hosted string) error {
	c := hostedCluster(mgmt, hosted)
	record, found, err := clusters.destroying(c)
	if err != nil {
		return err
	}
	if !found {
		p.done("Creation never started, nothing to destroy")
		return nil
	}
	params := hypershift.DestroyClusterParams{
		Name:       hosted,
		InfraID:    record.InfraID,
		Region:     record.Region,
		BaseDomain: record.BaseDomain,
	}
	var ready bool
	_ = p.wait(ctx, "Checking the management cluster API", func(ctx context.Context) error {
		ready = deps.Cluster.Ready(ctx, clusters.kubeconfig(managementCluster(mgmt)))
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
	return clusters.forget(c)
}

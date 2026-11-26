package app

import (
	"context"
	"time"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// ensureHostedCluster creates the hosted cluster with AutoNode and writes its
// kubeconfig, unless it is already created.
func ensureHostedCluster(ctx context.Context, p progress, hs Hypershift, dirs xdg.Dirs, opts createOptions) error {
	clusters := newClusterStore(dirs)
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

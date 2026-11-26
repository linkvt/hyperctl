package app

import (
	"context"
	"fmt"
	"time"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// ensureHostedCluster creates the hosted cluster with AutoNode and writes its
// kubeconfig, unless it is already created.
func ensureHostedCluster(ctx context.Context, p progress, hs Hypershift, dirs xdg.Dirs, opts createOptions) error {
	dir := hostedDir(dirs, opts.Name, opts.HostedName)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	switch {
	case found && record.Phase == phaseInstalled:
		p.done("Hosted cluster %s is already created", opts.HostedName)
		return nil
	case found:
		return fmt.Errorf("previous creation of hosted cluster %s did not complete, destroy it first", opts.HostedName)
	}

	// The infra ID is chosen and recorded before creating the cluster, so
	// that its AWS resources can be destroyed without the management
	// cluster, even if creating did not complete.
	record = clusterRecord{
		Platform:   platformAWS,
		Phase:      phaseInstalling,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
		InfraID:    hypershift.NewInfraID(opts.HostedName),
	}
	if err := saveClusterRecord(dir, record); err != nil {
		return err
	}
	p.info("Creating with AutoNode and waiting until it is available")
	start := time.Now()
	if err := hs.CreateCluster(ctx, hypershift.CreateClusterParams{
		Name:         opts.HostedName,
		InfraID:      record.InfraID,
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
	kubeconfigPath := hostedKubeconfigPath(dirs, opts.Name, opts.HostedName)
	if err := writeFile(kubeconfigPath, kubeconfig); err != nil {
		return err
	}
	p.done("Wrote kubeconfig %s", kubeconfigPath)
	record.Phase = phaseInstalled
	return saveClusterRecord(dir, record)
}

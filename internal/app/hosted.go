package app

import (
	"context"
	"fmt"
	"io"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// ensureHostedCluster creates the hosted cluster with AutoNode and writes its
// kubeconfig, unless it is already created.
func ensureHostedCluster(ctx context.Context, w io.Writer, hs hypershift.CLI, dirs xdg.Dirs, opts createOptions) error {
	dir := hostedDir(dirs, opts.Name, opts.HostedName)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	switch {
	case found && record.Phase == phaseInstalled:
		_, _ = fmt.Fprintf(w, "Hosted cluster %s is already created\n", opts.HostedName)
		return nil
	case found:
		return fmt.Errorf("previous creation of hosted cluster %s did not complete, destroy it first", opts.HostedName)
	}

	record = clusterRecord{
		Platform:   platformAWS,
		Phase:      phaseInstalling,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
	}
	if err := saveClusterRecord(dir, record); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "Creating hosted cluster %s with AutoNode, waiting until it is available\n", opts.HostedName)
	if err := hs.CreateCluster(ctx, hypershift.CreateClusterParams{
		Name:         opts.HostedName,
		Region:       opts.Region,
		BaseDomain:   opts.BaseDomain,
		PullSecret:   opts.PullSecret,
		ReleaseImage: ocp.Release{Version: opts.Release}.PullSpec(),
		SSHKey:       opts.SSHKey,
	}); err != nil {
		return err
	}
	kubeconfig, err := hs.HostedKubeconfig(ctx, opts.HostedName)
	if err != nil {
		return err
	}
	if err := writeFile(hostedKubeconfigPath(dirs, opts.Name, opts.HostedName), kubeconfig); err != nil {
		return err
	}
	record.Phase = phaseInstalled
	return saveClusterRecord(dir, record)
}

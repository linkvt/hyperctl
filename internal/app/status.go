package app

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/oc"
	"github.com/linkvt/hyperctl/internal/xdg"
)

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the state of the management and hosted cluster",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runStatus(ctx, cmd.Root().Writer)
		},
	}
}

func runStatus(ctx context.Context, w io.Writer) error {
	dirs, err := xdg.Resolve("hyperctl")
	if err != nil {
		return err
	}
	st, err := loadState(dirs.State)
	if err != nil {
		return err
	}
	if st.MgmtCluster == "" {
		_, err := fmt.Fprintln(w, "No clusters, create them with: hyperctl create")
		return err
	}

	mgmt, found, err := loadClusterRecord(clusterDir(dirs, st.MgmtCluster))
	if err != nil {
		return err
	}
	if !found {
		_, err := fmt.Fprintf(w, "Management cluster %s: not created\n", st.MgmtCluster)
		return err
	}
	kubeconfig := mgmtKubeconfigPath(dirs, st.MgmtCluster)
	ready := mgmt.Phase == phaseInstalled && oc.Ready(ctx, kubeconfig)
	api := "API reachable"
	if !ready {
		api = "API unreachable"
	}
	_, _ = fmt.Fprintf(w, "Management cluster %s: %s, release %s, region %s, %s\n", st.MgmtCluster, mgmt.Phase, mgmt.Release, mgmt.Region, api)
	if mgmt.Phase == phaseInstalled && !ready {
		_, _ = fmt.Fprintln(w, "  The instances may have been pruned, recreate the clusters with: hyperctl destroy && hyperctl create")
	}

	if st.HostedCluster == "" {
		return nil
	}
	hosted, found, err := loadClusterRecord(hostedDir(dirs, st.MgmtCluster, st.HostedCluster))
	if err != nil {
		return err
	}
	switch {
	case !found:
		_, _ = fmt.Fprintf(w, "Hosted cluster %s: not created\n", st.HostedCluster)
	case !ready:
		_, _ = fmt.Fprintf(w, "Hosted cluster %s: %s, Available=unknown\n", st.HostedCluster, hosted.Phase)
	default:
		available, err := oc.HostedClusterAvailable(ctx, kubeconfig, st.HostedCluster)
		if err != nil {
			available = "unknown (" + err.Error() + ")"
		}
		_, _ = fmt.Fprintf(w, "Hosted cluster %s: %s, Available=%s\n", st.HostedCluster, hosted.Phase, available)
	}
	return nil
}

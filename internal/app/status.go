package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/oc"
	"github.com/linkvt/hyperctl/internal/xdg"
)

func statusCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the state of the management and hosted cluster",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Progress goes to stderr, keeping stdout for the status.
			return runStatus(ctx, cmd.Root().Writer, progress{w: cmd.Root().ErrWriter, interactive: deps.Interactive})
		},
	}
}

func runStatus(ctx context.Context, w io.Writer, p progress) error {
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
	_, _ = fmt.Fprintf(w, "Management cluster %s\n", st.MgmtCluster)
	if !found {
		field(w, "Phase", "not created")
		return nil
	}
	kubeconfig := mgmtKubeconfigPath(dirs, st.MgmtCluster)
	var ready bool
	if mgmt.Phase == phaseInstalled {
		_ = p.wait(ctx, "Checking the management cluster API", func(ctx context.Context) error {
			ready = oc.Ready(ctx, kubeconfig)
			return nil
		})
	}
	printPhase(w, mgmt)
	field(w, "Release", mgmt.Release)
	field(w, "Region", mgmt.Region)
	switch {
	case mgmt.Phase != phaseInstalled:
		field(w, "Log", filepath.Join(installDir(dirs, st.MgmtCluster), ".openshift_install.log"))
	case ready:
		field(w, "API", "reachable")
		field(w, "Kubeconfig", kubeconfig)
	default:
		field(w, "API", "unreachable, the instances may have been pruned, recreate the clusters with: hyperctl destroy && hyperctl create")
	}

	if st.HostedCluster == "" {
		return nil
	}
	hosted, found, err := loadClusterRecord(hostedDir(dirs, st.MgmtCluster, st.HostedCluster))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\nHosted cluster %s\n", st.HostedCluster)
	if !found {
		field(w, "Phase", "not created")
		return nil
	}
	available := "unknown"
	var annotations map[string]string
	if ready {
		_ = p.wait(ctx, "Checking the hosted cluster", func(ctx context.Context) error {
			status, err := oc.HostedClusterAvailable(ctx, kubeconfig, st.HostedCluster)
			if err != nil {
				available = "unknown (" + err.Error() + ")"
				return nil
			}
			available = status
			// Overrides are optional information, errors show in availability.
			annotations, _ = oc.HostedClusterAnnotations(ctx, kubeconfig, st.HostedCluster)
			return nil
		})
	}
	printPhase(w, hosted)
	field(w, "Available", available)
	if hosted.Phase == phaseInstalled {
		field(w, "Kubeconfig", hostedKubeconfigPath(dirs, st.MgmtCluster, st.HostedCluster))
	}
	printImageOverrides(w, annotations)
	return nil
}

// printImageOverrides prints the image overrides set with hyperctl image.
func printImageOverrides(w io.Writer, annotations map[string]string) {
	header := false
	for _, component := range imageComponents {
		image, set := annotations[component.annotation]
		if !set {
			continue
		}
		if !header {
			_, _ = fmt.Fprintln(w, "  Image overrides:")
			header = true
		}
		_, _ = fmt.Fprintf(w, "    %-20s%s\n", component.title+":", image)
	}
}

// printPhase prints the phase of a cluster with the time it was reached and,
// for incomplete clusters, how to proceed.
func printPhase(w io.Writer, r clusterRecord) {
	field(w, "Phase", fmt.Sprintf("%s (since %s, %s ago)",
		r.Phase, r.UpdatedAt.Local().Format("2006-01-02 15:04"), since(r.UpdatedAt)))
	if r.Phase != phaseInstalled {
		field(w, "Note", "unless hyperctl create is still running, the installation failed, destroy it with: hyperctl destroy")
	}
}

func field(w io.Writer, name, value string) {
	_, _ = fmt.Fprintf(w, "  %-12s%s\n", name+":", value)
}

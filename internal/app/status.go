package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func statusCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the state of the management and hosted cluster",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Progress goes to stderr, keeping stdout for the status.
			return runStatus(ctx, deps.Cluster, cmd.Root().Writer, progress{w: cmd.Root().ErrWriter, interactive: deps.Interactive})
		},
	}
}

func runStatus(ctx context.Context, cluster ClusterAPI, w io.Writer, p progress) error {
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

	clusters := newClusterStore(dirs)
	mgmtRef := managementCluster(st.MgmtCluster)
	mgmt, found, err := clusters.load(mgmtRef)
	if err != nil {
		return err
	}
	if !found {
		_, _ = fmt.Fprintf(w, "%s %s\n", st.Setup.clusterTitle(), st.MgmtCluster)
		field(w, "Phase", "not created")
		return nil
	}
	title, operator := "Management cluster", "installed"
	if !mgmt.hasOperator() {
		title, operator = "Cluster", "not installed"
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", title, st.MgmtCluster)
	kubeconfig := clusters.kubeconfig(mgmtRef)
	var ready bool
	if mgmt.Phase == phaseInstalled {
		_ = p.wait(ctx, "Checking the management cluster API", func(ctx context.Context) error {
			ready = cluster.Ready(ctx, kubeconfig)
			return nil
		})
	}
	printPhase(w, mgmt, "hyperctl destroy")
	field(w, "Platform", mgmt.Platform)
	if mgmt.Release != "" {
		field(w, "Release", mgmt.Release)
	}
	field(w, "Region", mgmt.Region)
	field(w, "HyperShift", operator)
	switch {
	case mgmt.Phase != phaseInstalled:
		field(w, "Log", filepath.Join(clusters.installDir(mgmtRef), ".openshift_install.log"))
	case ready:
		field(w, "API", "reachable")
		field(w, "Kubeconfig", kubeconfig)
	default:
		field(w, "API", "unreachable, the instances may have been pruned, recreate the clusters with: hyperctl destroy && hyperctl create")
	}

	if st.HostedCluster == "" {
		return nil
	}
	hostedRef := hostedCluster(st.MgmtCluster, st.HostedCluster)
	hosted, found, err := clusters.load(hostedRef)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\nHosted cluster %s\n", st.HostedCluster)
	if !found {
		field(w, "Phase", "not created")
		return nil
	}
	available := "unknown"
	overrides := map[string]string{}
	if ready {
		_ = p.wait(ctx, "Checking the hosted cluster", func(ctx context.Context) error {
			status, err := cluster.HostedClusterAvailable(ctx, kubeconfig, st.HostedCluster)
			if err != nil {
				available = "unknown (" + err.Error() + ")"
				return nil
			}
			available = status
			overrides = imageOverrides(ctx, cluster, kubeconfig, st.HostedCluster)
			return nil
		})
	}
	printPhase(w, hosted, "hyperctl destroy hosted")
	field(w, "Available", available)
	if hosted.Phase == phaseInstalled {
		field(w, "Kubeconfig", clusters.kubeconfig(hostedRef))
	}
	printImageOverrides(w, overrides)
	return nil
}

// imageOverrides returns the image overrides by component name. Errors are
// ignored, overrides are optional information.
func imageOverrides(ctx context.Context, cluster ClusterAPI, kubeconfig, hosted string) map[string]string {
	overrides := map[string]string{}
	for _, component := range imageComponents {
		annotations, err := cluster.Annotations(ctx, kubeconfig, component.kind, component.namespace(hosted), hosted)
		if err != nil {
			continue
		}
		if image, set := annotations[component.annotation]; set {
			overrides[component.name] = image
		}
	}
	return overrides
}

// printImageOverrides prints the image overrides set with hyperctl image.
func printImageOverrides(w io.Writer, overrides map[string]string) {
	header := false
	for _, component := range imageComponents {
		image, set := overrides[component.name]
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
func printPhase(w io.Writer, r clusterRecord, destroyCommand string) {
	field(w, "Phase", fmt.Sprintf("%s (since %s, %s ago)",
		r.Phase, r.UpdatedAt.Local().Format("2006-01-02 15:04"), since(r.UpdatedAt)))
	if r.Phase != phaseInstalled {
		field(w, "Note", fmt.Sprintf("unless hyperctl create is still running, the installation failed, destroy it with: %s", destroyCommand))
	}
}

func field(w io.Writer, name, value string) {
	_, _ = fmt.Fprintf(w, "  %-12s%s\n", name+":", value)
}

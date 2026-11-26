package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/urfave/cli/v3"
)

func statusCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the state of the cluster and its management or hosted clusters",
		Flags: []cli.Flag{clusterFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Progress goes to stderr, keeping stdout for the status.
			return runStatus(ctx, cmd, deps.Cluster, cmd.Root().Writer, progress{w: cmd.Root().ErrWriter, interactive: deps.Interactive})
		},
	}
}

// runStatus shows the selected cluster with its relatives: a management
// cluster with its hosted clusters, a hosted cluster with its management
// cluster.
func runStatus(ctx context.Context, cmd *cli.Command, cluster ClusterAPI, w io.Writer, p progress) error {
	if err := noArguments(cmd); err != nil {
		return err
	}
	sel, err := selectCluster(cmd)
	if err != nil {
		return err
	}
	mgmt := sel.management()
	hosted := sel.hostedClusters()

	if !mgmt.recorded {
		// Without a record, nothing tells whether it was to get the
		// HyperShift operator.
		_, _ = fmt.Fprintf(w, "Cluster %s\n", mgmt.mgmt)
		field(w, "Phase", "not created")
		return nil
	}
	title, operator := "Management cluster", "installed"
	if !mgmt.record.hasOperator() {
		title, operator = "Cluster", "not installed"
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", title, mgmt.mgmt)
	kubeconfig := sel.store.kubeconfig(mgmt.clusterRef)
	var ready bool
	if mgmt.record.Phase == phaseInstalled {
		_ = p.wait(ctx, "Checking the management cluster API", func(ctx context.Context) error {
			ready = cluster.Ready(ctx, kubeconfig)
			return nil
		})
	}
	printPhase(w, mgmt.record, "hyperctl destroy --cluster "+mgmt.mgmt)
	field(w, "Platform", mgmt.record.Platform)
	if mgmt.record.Release != "" {
		field(w, "Release", mgmt.record.Release)
	}
	field(w, "Region", mgmt.record.Region)
	field(w, "HyperShift", operator)
	switch {
	case mgmt.record.Phase != phaseInstalled:
		field(w, "Log", filepath.Join(sel.store.installDir(mgmt.clusterRef), ".openshift_install.log"))
	case ready:
		field(w, "API", "reachable")
		field(w, "Kubeconfig", kubeconfig)
	default:
		field(w, "API", fmt.Sprintf("unreachable, the instances may have been pruned, recreate the cluster with: hyperctl destroy --cluster %s && hyperctl create --name %s", mgmt.mgmt, mgmt.mgmt))
	}

	for _, h := range hosted {
		printHostedStatus(ctx, w, p, cluster, sel.store, kubeconfig, ready, h)
	}
	return nil
}

// printHostedStatus shows a hosted cluster, with the live checks if its
// management cluster is ready.
func printHostedStatus(ctx context.Context, w io.Writer, p progress, cluster ClusterAPI, clusters clusterStore, kubeconfig string, ready bool, hosted storedCluster) {
	_, _ = fmt.Fprintf(w, "\nHosted cluster %s\n", hosted.hosted)
	if !hosted.recorded {
		field(w, "Phase", "not created")
		return
	}
	available := "unknown"
	overrides := map[string]string{}
	if ready {
		_ = p.wait(ctx, "Checking hosted cluster "+hosted.hosted, func(ctx context.Context) error {
			status, err := cluster.HostedClusterAvailable(ctx, kubeconfig, hosted.hosted)
			if err != nil {
				available = "unknown (" + err.Error() + ")"
				return nil
			}
			available = status
			overrides = imageOverrides(ctx, cluster, kubeconfig, hosted.hosted)
			return nil
		})
	}
	printPhase(w, hosted.record, "hyperctl destroy --cluster "+hosted.hosted)
	field(w, "Available", available)
	if hosted.record.Phase == phaseInstalled {
		field(w, "Kubeconfig", clusters.kubeconfig(hosted.clusterRef))
	}
	printImageOverrides(w, overrides)
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

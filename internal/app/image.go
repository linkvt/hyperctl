package app

import (
	"context"
	"errors"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// imageComponent is a hosted control plane component whose image can be
// overridden with an annotation.
type imageComponent struct {
	name  string
	title string
	// kind is the object that carries the annotation, the HostedCluster or
	// its HostedControlPlane.
	kind       string
	annotation string
	// selector matches the component's pods in the control plane namespace.
	selector string
}

var imageComponents = []imageComponent{
	{
		name:       "karpenter-operator",
		title:      "Karpenter operator",
		kind:       "hostedcluster",
		annotation: "hypershift.openshift.io/karpenter-operator-image",
		selector:   "app=karpenter-operator",
	},
	{
		name:  "karpenter-provider",
		title: "Karpenter provider",
		// The standalone karpenter-operator deploys the provider on both
		// clouds and reads the image from the HostedControlPlane it runs for.
		kind:       "hostedcontrolplane",
		annotation: "autoscaling.openshift.io/karpenter-image",
		selector:   "app=karpenter",
	},
}

func imageCommand(deps Deps) *cli.Command {
	var commands []*cli.Command
	for _, component := range imageComponents {
		commands = append(commands, &cli.Command{
			Name:      component.name,
			Usage:     "Override the " + component.title + " image of the hosted cluster",
			ArgsUsage: "<image>",
			Flags: []cli.Flag{
				&cli.BoolFlag{Name: "reset", Usage: "Remove the override and use the image of the release"},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return runImage(ctx, cmd, deps.Cluster, component)
			},
		})
	}
	return &cli.Command{
		Name:     "image",
		Usage:    "Override images of hosted control plane components",
		Commands: commands,
	}
}

func runImage(ctx context.Context, cmd *cli.Command, cluster ClusterAPI, component imageComponent) error {
	image := cmd.Args().First()
	reset := cmd.Bool("reset")
	if (image == "") == !reset {
		return errors.New("expected an image or --reset")
	}

	dirs, err := xdg.Resolve("hyperctl")
	if err != nil {
		return err
	}
	st, err := loadState(dirs.State)
	if err != nil {
		return err
	}
	if st.HostedCluster == "" {
		return errors.New("no hosted cluster, run hyperctl create")
	}
	kubeconfig := newClusterStore(dirs).kubeconfig(managementCluster(st.MgmtCluster))
	namespace := component.namespace(st.HostedCluster)
	annotations, err := cluster.Annotations(ctx, kubeconfig, component.kind, namespace, st.HostedCluster)
	if err != nil {
		return err
	}
	current, set := annotations[component.annotation]
	p := progress{w: cmd.Root().Writer}

	switch {
	case reset && !set:
		p.done("No %s image override set", component.title)
	case reset:
		if err := cluster.RemoveAnnotation(ctx, kubeconfig, component.kind, namespace, st.HostedCluster, component.annotation); err != nil {
			return err
		}
		p.done("Removed the %s image override, the deployment rolls out with the release image", component.title)
	case current != image:
		if err := cluster.Annotate(ctx, kubeconfig, component.kind, namespace, st.HostedCluster, component.annotation, image); err != nil {
			return err
		}
		p.done("Set the %s image to %s, the deployment rolls out", component.title, image)
	default:
		return restartPods(ctx, p, cluster, kubeconfig, st.HostedCluster, component)
	}
	return nil
}

// namespace returns the namespace of the object that carries the override.
func (c imageComponent) namespace(hosted string) string {
	if c.kind == "hostedcontrolplane" {
		return controlPlaneNamespace(hosted)
	}
	return "clusters"
}

// restartPods deletes the component's pods, so that they start with the
// unchanged image again. Nodes may serve it from their image cache, only a
// unique tag reliably rolls out a rebuilt image.
func restartPods(ctx context.Context, p progress, cluster ClusterAPI, kubeconfig, hosted string, component imageComponent) error {
	if err := cluster.DeletePods(ctx, kubeconfig, controlPlaneNamespace(hosted), component.selector); err != nil {
		return err
	}
	p.done("The %s image is unchanged, restarted its pods", component.title)
	p.warn("The pods may keep a cached image, push the image with a unique tag")
	return nil
}

// controlPlaneNamespace is the namespace of a hosted cluster's control plane.
func controlPlaneNamespace(hosted string) string {
	return "clusters-" + strings.ReplaceAll(hosted, ".", "-")
}

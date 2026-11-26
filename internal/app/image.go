package app

import (
	"context"
	"errors"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/oc"
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

func imageCommand() *cli.Command {
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
				return runImage(ctx, cmd, component)
			},
		})
	}
	return &cli.Command{
		Name:     "image",
		Usage:    "Override images of hosted control plane components",
		Commands: commands,
	}
}

func runImage(ctx context.Context, cmd *cli.Command, component imageComponent) error {
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
	kubeconfig := mgmtKubeconfigPath(dirs, st.MgmtCluster)
	namespace := component.namespace(st.HostedCluster)
	annotations, err := oc.Annotations(ctx, kubeconfig, component.kind, namespace, st.HostedCluster)
	if err != nil {
		return err
	}
	current, set := annotations[component.annotation]
	p := progress{w: cmd.Root().Writer}

	switch {
	case reset && !set:
		p.done("No %s image override set", component.title)
	case reset:
		if err := oc.RemoveAnnotation(ctx, kubeconfig, component.kind, namespace, st.HostedCluster, component.annotation); err != nil {
			return err
		}
		p.done("Removed the %s image override, the deployment rolls out with the release image", component.title)
	case current != image:
		if err := oc.Annotate(ctx, kubeconfig, component.kind, namespace, st.HostedCluster, component.annotation, image); err != nil {
			return err
		}
		p.done("Set the %s image to %s, the deployment rolls out", component.title, image)
	default:
		return restartPods(ctx, p, kubeconfig, st.HostedCluster, component, image)
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

// restartPods deletes the component's pods, so that they pull the unchanged
// image again. Only :latest images are always pulled by hypershift, other
// tags use the image cached on the node.
func restartPods(ctx context.Context, p progress, kubeconfig, hosted string, component imageComponent, image string) error {
	if err := oc.DeletePods(ctx, kubeconfig, controlPlaneNamespace(hosted), component.selector); err != nil {
		return err
	}
	p.done("The %s image is unchanged, restarted its pods", component.title)
	if !strings.HasSuffix(image, ":latest") {
		p.warn("The pods use imagePullPolicy IfNotPresent for tags other than :latest and may keep a cached image, push a new tag or use :latest")
	}
	return nil
}

// controlPlaneNamespace is the namespace of a hosted cluster's control plane.
func controlPlaneNamespace(hosted string) string {
	return "clusters-" + strings.ReplaceAll(hosted, ".", "-")
}

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/oc"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// imageComponent is a hosted control plane component whose image can be
// overridden with a HostedCluster annotation.
type imageComponent struct {
	name       string
	title      string
	annotation string
	// selector matches the component's pods in the control plane namespace.
	selector string
	// awsOnly is set for components whose annotation hypershift only
	// supports on AWS.
	awsOnly bool
}

var imageComponents = []imageComponent{
	{
		name:       "karpenter-operator",
		title:      "Karpenter operator",
		annotation: "hypershift.openshift.io/karpenter-operator-image",
		selector:   "app=karpenter-operator",
	},
	{
		name:       "karpenter-provider",
		title:      "Karpenter provider",
		annotation: "hypershift.openshift.io/karpenter-provider-aws-image",
		selector:   "app=karpenter",
		awsOnly:    true,
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
	platform, err := trackedPlatform(dirs, st)
	if err != nil {
		return err
	}
	if component.awsOnly && platform != platformAWS {
		return fmt.Errorf("hypershift supports overriding the %s image only on AWS", component.title)
	}
	kubeconfig := mgmtKubeconfigPath(dirs, st.MgmtCluster)
	annotations, err := oc.HostedClusterAnnotations(ctx, kubeconfig, st.HostedCluster)
	if err != nil {
		return err
	}
	current, set := annotations[component.annotation]
	p := progress{w: cmd.Root().Writer}

	switch {
	case reset && !set:
		p.done("No %s image override set", component.title)
	case reset:
		if err := oc.RemoveHostedClusterAnnotation(ctx, kubeconfig, st.HostedCluster, component.annotation); err != nil {
			return err
		}
		p.done("Removed the %s image override, the deployment rolls out with the release image", component.title)
	case current != image:
		if err := oc.AnnotateHostedCluster(ctx, kubeconfig, st.HostedCluster, component.annotation, image); err != nil {
			return err
		}
		p.done("Set the %s image to %s, the deployment rolls out", component.title, image)
	default:
		return restartPods(ctx, p, kubeconfig, st.HostedCluster, component, image)
	}
	return nil
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

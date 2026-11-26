package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

func kubeconfigCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:  "kubeconfig",
		Usage: "Print the kubeconfig path of the cluster, usable as KUBECONFIG: for a hosted cluster, or a management cluster with one hosted cluster, the hosted cluster's followed by the management cluster's",
		Flags: []cli.Flag{clusterFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := noArguments(cmd); err != nil {
				return err
			}
			sel, err := selectCluster(cmd)
			if err != nil {
				return err
			}
			c, required := sel.cluster.clusterRef, true
			if !c.isHosted() {
				switch hosted := sel.clusters.hosted(c.mgmt); len(hosted) {
				case 0:
				case 1:
					c, required = hosted[0].clusterRef, false
				default:
					p := progress{w: cmd.Root().ErrWriter}
					p.info("Hosted clusters of %s, select one with --cluster: %s", c.mgmt, strings.Join(hosted.names(), ", "))
				}
			}
			kubeconfigs, err := selectKubeconfigs(ctx, deps.Cluster, sel.store, c, required)
			if err != nil {
				return err
			}
			var paths []string
			for _, k := range kubeconfigs {
				if err := nameKubeconfig(k.path, k.cluster); err != nil {
					return err
				}
				paths = append(paths, k.path)
			}
			_, err = fmt.Fprintln(cmd.Root().Writer, strings.Join(paths, string(os.PathListSeparator)))
			return err
		},
	}
}

// kubeconfigFile is the kubeconfig of a cluster.
type kubeconfigFile struct {
	cluster string
	path    string
}

// selectKubeconfigs returns the kubeconfig of a management or standalone
// cluster, or the ones of a hosted cluster and its management cluster with
// the hosted cluster first, which makes it the current context of a merged
// KUBECONFIG. The kubeconfig of a hosted cluster is fetched from its
// management cluster once published. Until then, it fails if the hosted
// cluster is required and leaves it out otherwise.
func selectKubeconfigs(ctx context.Context, api ClusterAPI, clusters clusterStore, c clusterRef, required bool) ([]kubeconfigFile, error) {
	mgmt := kubeconfigFile{cluster: c.mgmt, path: clusters.kubeconfig(managementCluster(c.mgmt))}
	if !c.isHosted() {
		return []kubeconfigFile{mgmt}, nil
	}
	hosted := kubeconfigFile{cluster: c.hosted, path: clusters.kubeconfig(c)}
	_, err := os.Stat(hosted.path)
	if err == nil {
		return []kubeconfigFile{hosted, mgmt}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	content, err := api.HostedKubeconfig(ctx, mgmt.path, c.hosted)
	if err != nil {
		return nil, err
	}
	switch {
	case len(content) == 0 && required:
		return nil, errors.New("hosted kubeconfig not available yet")
	case len(content) == 0:
		return []kubeconfigFile{mgmt}, nil
	}
	if err := writeFile(hosted.path, content); err != nil {
		return nil, err
	}
	return []kubeconfigFile{hosted, mgmt}, nil
}

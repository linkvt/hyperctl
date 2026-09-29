package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func kubeconfigCommand() *cli.Command {
	return &cli.Command{
		Name:      "kubeconfig",
		Usage:     "Print the kubeconfig paths of the hosted and management cluster, usable as KUBECONFIG",
		ArgsUsage: "[hosted|mgmt]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			dirs, err := xdg.Resolve("hyperctl")
			if err != nil {
				return err
			}
			s, err := loadState(dirs.State)
			if err != nil {
				return err
			}
			kubeconfigs, err := selectKubeconfigs(dirs, s, cmd.Args().First())
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

// selectKubeconfigs returns the kubeconfig of the hosted or management
// cluster, or of both with the hosted cluster first, which makes it the
// current context of a merged KUBECONFIG.
func selectKubeconfigs(dirs xdg.Dirs, s state, cluster string) ([]kubeconfigFile, error) {
	if s.MgmtCluster == "" {
		return nil, errors.New("no management cluster, run hyperctl create")
	}
	mgmt := kubeconfigFile{cluster: s.MgmtCluster, path: mgmtKubeconfigPath(dirs, s.MgmtCluster)}
	hosted := kubeconfigFile{cluster: s.HostedCluster, path: hostedKubeconfigPath(dirs, s.MgmtCluster, s.HostedCluster)}
	hostedExists := false
	if s.HostedCluster != "" {
		_, err := os.Stat(hosted.path)
		hostedExists = err == nil
	}

	switch cluster {
	case "":
		if hostedExists {
			return []kubeconfigFile{hosted, mgmt}, nil
		}
		return []kubeconfigFile{mgmt}, nil
	case "hosted":
		if !hostedExists {
			return nil, errors.New("no hosted cluster, run hyperctl create")
		}
		return []kubeconfigFile{hosted}, nil
	case "mgmt":
		return []kubeconfigFile{mgmt}, nil
	default:
		return nil, fmt.Errorf("unknown cluster %q, expected hosted or mgmt", cluster)
	}
}

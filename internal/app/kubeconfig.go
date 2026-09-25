package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func kubeconfigCommand() *cli.Command {
	return &cli.Command{
		Name:      "kubeconfig",
		Usage:     "Print the kubeconfig path of the hosted (default) or management cluster",
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
			path, err := kubeconfigPath(dirs, s, cmd.Args().First())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.Root().Writer, path)
			return err
		},
	}
}

func kubeconfigPath(dirs xdg.Dirs, s state, cluster string) (string, error) {
	switch cluster {
	case "", "hosted":
		if s.HostedCluster == "" {
			return "", errors.New("no hosted cluster, run hyperctl create")
		}
		return hostedKubeconfigPath(dirs, s.MgmtCluster, s.HostedCluster), nil
	case "mgmt":
		if s.MgmtCluster == "" {
			return "", errors.New("no management cluster, run hyperctl create")
		}
		return mgmtKubeconfigPath(dirs, s.MgmtCluster), nil
	default:
		return "", fmt.Errorf("unknown cluster %q, expected hosted or mgmt", cluster)
	}
}

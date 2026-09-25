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
		Usage:     "Print the kubeconfig path of a cluster",
		ArgsUsage: "mgmt",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().First() != "mgmt" {
				return errors.New(`expected cluster "mgmt"`)
			}
			dirs, err := xdg.Resolve("hyperctl")
			if err != nil {
				return err
			}
			s, err := loadState(dirs.State)
			if err != nil {
				return err
			}
			if s.MgmtCluster == "" {
				return errors.New("no management cluster, run hyperctl create")
			}
			_, err = fmt.Fprintln(cmd.Root().Writer, mgmtKubeconfigPath(dirs, s.MgmtCluster))
			return err
		},
	}
}

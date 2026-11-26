package app

import (
	"context"
	"errors"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func useCommand() *cli.Command {
	return &cli.Command{
		Name:      "use",
		Usage:     "Make a cluster the current cluster, which commands act on unless --cluster names one",
		ArgsUsage: "<name>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			name := cmd.Args().First()
			if name == "" || cmd.Args().Len() > 1 {
				return errors.New("expected the name of a cluster, see hyperctl list")
			}
			dirs, err := xdg.Resolve("hyperctl")
			if err != nil {
				return err
			}
			clusters, err := newClusterStore(dirs).list()
			if err != nil {
				return err
			}
			if _, found := clusters.find(name); !found {
				return unknownClusterError(name, clusters)
			}
			if err := updateState(dirs.State, func(st *state) { st.Current = name }); err != nil {
				return err
			}
			progress{w: cmd.Root().Writer}.done("Current cluster %s", name)
			return nil
		},
	}
}

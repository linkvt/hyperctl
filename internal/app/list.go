package app

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func listCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List the clusters, marking the current one with *",
		Action: func(_ context.Context, cmd *cli.Command) error {
			dirs, err := xdg.Resolve("hyperctl")
			if err != nil {
				return err
			}
			st, err := loadState(dirs.State)
			if err != nil {
				return err
			}
			clusters, err := newClusterStore(dirs).list()
			if err != nil {
				return err
			}
			return printClusters(cmd.Root().Writer, clusters, st.Current)
		},
	}
}

// printClusters prints the clusters from their records, with the hosted
// clusters below their management cluster.
func printClusters(w io.Writer, clusters clusterList, current string) error {
	if len(clusters) == 0 {
		_, err := fmt.Fprintln(w, "No clusters, create one with: hyperctl create")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  NAME\tPLATFORM\tSETUP\tPHASE")
	for _, c := range clusters {
		marker := "  "
		if c.name() == current {
			marker = "* "
		}
		name, platform, setup, phase := c.mgmt, c.record.Platform, string(setupManagement), string(c.record.Phase)
		if !c.record.hasOperator() {
			setup = string(setupStandalone)
		}
		if c.isHosted() {
			name, platform, setup = "└─ "+c.hosted, "", ""
		}
		if !c.recorded {
			platform, setup, phase = "", "", "not created"
		}
		_, _ = fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\n", marker, name, platform, setup, phase)
	}
	return tw.Flush()
}

package app

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/releases"
)

func releasesCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:      "releases",
		Usage:     "List release streams, or the accepted releases of a stream",
		ArgsUsage: "[stream]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "all", Usage: "Include releases that are not accepted"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client := releases.Client{BaseURL: deps.ReleasesURL}
			stream := cmd.Args().First()
			if stream == "" {
				streams, err := client.Streams(ctx)
				if err != nil {
					return err
				}
				for _, s := range streams {
					_, _ = fmt.Fprintln(cmd.Root().Writer, s)
				}
				return nil
			}

			tags, err := client.Tags(ctx, stream)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.Root().Writer, 0, 0, 2, ' ', 0)
			for _, tag := range filterTags(tags, cmd.Bool("all")) {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", tag.Name, tag.Phase, tag.PullSpec)
			}
			return w.Flush()
		},
	}
}

// filterTags returns the accepted tags, or all tags if all is set.
func filterTags(tags []releases.Tag, all bool) []releases.Tag {
	if all {
		return tags
	}
	var accepted []releases.Tag
	for _, tag := range tags {
		if tag.Phase == releases.PhaseAccepted {
			accepted = append(accepted, tag)
		}
	}
	return accepted
}

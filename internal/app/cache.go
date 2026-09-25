package app

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func cacheCommand() *cli.Command {
	return &cli.Command{
		Name:  "cache",
		Usage: "Manage cached installer binaries",
		Commands: []*cli.Command{{
			Name:  "clean",
			Usage: "Remove cached installer binaries, they are extracted again when needed",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				dirs, err := xdg.Resolve("hyperctl")
				if err != nil {
					return err
				}
				if err := os.RemoveAll(dirs.Cache); err != nil {
					return err
				}
				_, err = fmt.Fprintf(cmd.Root().Writer, "Removed %s\n", dirs.Cache)
				return err
			},
		}},
	}
}

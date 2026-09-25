package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/aws/aws-sdk-go-v2/config"
	"golang.org/x/term"

	"github.com/linkvt/hyperctl/internal/app"
	"github.com/linkvt/hyperctl/internal/awsaccount"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	cmd := app.New(app.Deps{
		AWS:           awsaccount.New(awsConfig),
		DefaultRegion: awsConfig.Region,
		Interactive:   term.IsTerminal(int(os.Stdin.Fd())),
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	})
	return cmd.Run(ctx, os.Args)
}

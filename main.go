package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"

	"github.com/aws/aws-sdk-go-v2/config"
	"golang.org/x/term"

	"github.com/linkvt/hyperctl/internal/app"
	"github.com/linkvt/hyperctl/internal/awsaccount"
	"github.com/linkvt/hyperctl/internal/azureaccount"
	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/releases"
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
	azureAccount, err := azureaccount.New()
	if err != nil {
		return err
	}
	cmd := app.New(app.Deps{
		AWS:   awsaccount.New(awsConfig),
		Azure: azureAccount,
		Hypershift: func(path, kubeconfig string, out io.Writer) app.Hypershift {
			return hypershift.New(path, kubeconfig, out)
		},
		LookPath:      exec.LookPath,
		ReleaseImages: ocp.New(),
		DefaultRegion: awsConfig.Region,
		ReleasesURL:   releases.DefaultURL,
		Interactive:   term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())),
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	})
	return cmd.Run(ctx, os.Args)
}

package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// resolveHypershift returns the path of a working hypershift binary: from the
// flag, the config file or PATH. When interactive and none is found, it asks
// for the path once and saves it to the config file.
func resolveHypershift(ctx context.Context, cmd *cli.Command, deps Deps, dirs xdg.Dirs, cfg config) (string, error) {
	path := flagOr(cmd, "hypershift", cfg.Hypershift)
	if path == "" {
		path, _ = exec.LookPath("hypershift")
	}
	if path == "" && deps.Interactive {
		var err error
		if path, err = promptHypershiftPath(ctx); err != nil {
			return "", err
		}
		cfg.Hypershift = path
		if err := saveConfig(dirs.Config, cfg); err != nil {
			return "", err
		}
	}
	if path == "" {
		return "", errors.New("hypershift binary not found in PATH, build it from github.com/openshift/hypershift (make build) and pass --hypershift or set hypershift in the config file")
	}

	path, err := expandHome(path)
	if err != nil {
		return "", err
	}
	if out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput(); err != nil {
		return "", fmt.Errorf("running %s --version: %w\n%s", path, err, out)
	}
	return path, nil
}

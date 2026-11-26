package app

import (
	"context"
	"errors"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// resolveHypershift returns the path of a working hypershift binary: from the
// flag, the config file or PATH. If none is found and it may prompt, it asks
// for the path once and saves it to the config file.
func resolveHypershift(ctx context.Context, cmd *cli.Command, deps Deps, dirs xdg.Dirs, cfg config) (string, error) {
	path := flagOr(cmd, "hypershift", cfg.Hypershift)
	if path == "" {
		path, _ = deps.LookPath("hypershift")
	}
	if path != "" {
		return verifyHypershift(ctx, deps, path)
	}
	if !prompts(cmd, deps) {
		return "", errors.New("hypershift binary not found in PATH, build it from github.com/openshift/hypershift (make build) and pass --hypershift or set hypershift in the config file")
	}

	path, err := promptHypershiftPath(ctx)
	if err != nil {
		return "", err
	}
	if path, err = verifyHypershift(ctx, deps, path); err != nil {
		return "", err
	}
	cfg.Hypershift = path
	if err := saveConfig(dirs.Config, cfg); err != nil {
		return "", err
	}
	return path, nil
}

// verifyHypershift expands the path and checks that the binary runs.
func verifyHypershift(ctx context.Context, deps Deps, path string) (string, error) {
	path, err := expandHome(path)
	if err != nil {
		return "", err
	}
	if err := deps.Hypershift(path, "", io.Discard).Version(ctx); err != nil {
		return "", err
	}
	return path, nil
}

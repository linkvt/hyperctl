package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// ensureMgmtCluster installs the management cluster unless it is installed.
func ensureMgmtCluster(ctx context.Context, cmd *cli.Command, dirs xdg.Dirs, opts createOptions) error {
	record, found, err := loadClusterRecord(clusterDir(dirs, opts.Name))
	if err != nil {
		return err
	}
	switch {
	case found && record.Phase == phaseInstalled:
		_, _ = fmt.Fprintf(cmd.Root().Writer, "Management cluster %s is already installed\n", opts.Name)
		return nil
	case found:
		return fmt.Errorf("previous installation of management cluster %s did not complete, destroy it first", opts.Name)
	}
	return installMgmtCluster(ctx, cmd, dirs, opts)
}

// installMgmtCluster extracts the installer for the release and installs the
// management cluster, tracking progress in the cluster record.
func installMgmtCluster(ctx context.Context, cmd *cli.Command, dirs xdg.Dirs, opts createOptions) error {
	release := ocp.ParseRelease(opts.Release)

	pullSecret, err := os.ReadFile(opts.PullSecret)
	if err != nil {
		return fmt.Errorf("reading pull secret: %w", err)
	}
	var sshKey []byte
	if opts.SSHKey != "" {
		if sshKey, err = os.ReadFile(opts.SSHKey); err != nil {
			return fmt.Errorf("reading SSH key: %w", err)
		}
	}

	installer, err := ocp.EnsureInstaller(ctx, filepath.Join(dirs.Cache, "releases"), release, opts.PullSecret)
	if err != nil {
		return err
	}

	installConfig, err := ocp.RenderInstallConfig(ocp.InstallConfigParams{
		Name:       opts.Name,
		BaseDomain: opts.BaseDomain,
		Region:     opts.Region,
		PullSecret: strings.TrimSpace(string(pullSecret)),
		SSHKey:     strings.TrimSpace(string(sshKey)),
	})
	if err != nil {
		return err
	}

	clusterDir := clusterDir(dirs, opts.Name)
	installDir := installDir(dirs, opts.Name)
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		return err
	}
	// The installer consumes install-config.yaml, keep a copy for reference.
	for _, dir := range []string{clusterDir, installDir} {
		if err := os.WriteFile(filepath.Join(dir, "install-config.yaml"), installConfig, 0o600); err != nil {
			return err
		}
	}

	record := clusterRecord{
		Platform:   platformAWS,
		Phase:      phaseInstalling,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
		PullSecret: opts.PullSecret,
	}
	if err := saveClusterRecord(clusterDir, record); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.Root().Writer, "Installing management cluster %s, this takes about 45 minutes (log: %s)\n",
		opts.Name, filepath.Join(installDir, ".openshift_install.log"))
	if err := installer.CreateCluster(ctx, installDir, release, cmd.Root().Writer, cmd.Root().ErrWriter); err != nil {
		return err
	}
	record.Phase = phaseInstalled
	return saveClusterRecord(clusterDir, record)
}

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// ensureMgmtCluster installs the management cluster unless it is installed.
func ensureMgmtCluster(ctx context.Context, p progress, account AWSAccount, dirs xdg.Dirs, opts createOptions) error {
	record, found, err := loadClusterRecord(clusterDir(dirs, opts.Name))
	if err != nil {
		return err
	}
	switch {
	case found && record.Phase == phaseInstalled:
		p.done("Management cluster %s is already installed", opts.Name)
		return nil
	case found:
		return fmt.Errorf("previous installation of management cluster %s did not complete, destroy it first", opts.Name)
	}
	return installMgmtCluster(ctx, p, account, dirs, opts)
}

// installMgmtCluster extracts the installer for the release and installs the
// management cluster, tracking progress in the cluster record.
func installMgmtCluster(ctx context.Context, p progress, account AWSAccount, dirs xdg.Dirs, opts createOptions) error {
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

	installer, err := ensureInstaller(ctx, p, dirs, release, opts.PullSecret)
	if err != nil {
		return err
	}

	var zone string
	if opts.Workers == 0 || opts.Workers == compactWorkerCount {
		err := p.wait(ctx, "Choosing an availability zone", func(ctx context.Context) error {
			var err error
			zone, err = account.FirstZone(ctx, opts.Region)
			return err
		})
		if err != nil {
			return err
		}
		p.done("Compact cluster in availability zone %s", zone)
	}

	installConfig, err := ocp.RenderInstallConfig(ocp.InstallConfigParams{
		Name:         opts.Name,
		BaseDomain:   opts.BaseDomain,
		Region:       opts.Region,
		Zone:         zone,
		InstanceType: opts.InstanceType,
		Workers:      opts.Workers,
		PullSecret:   strings.TrimSpace(string(pullSecret)),
		SSHKey:       strings.TrimSpace(string(sshKey)),
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
	p.info("Installing, this takes about 45 minutes (log: %s)", filepath.Join(installDir, ".openshift_install.log"))
	start := time.Now()
	if err := ocp.New().CreateCluster(ctx, installer, installDir, release, p.tool("openshift-install")); err != nil {
		return err
	}
	record.Phase = phaseInstalled
	if err := saveClusterRecord(clusterDir, record); err != nil {
		return err
	}
	p.done("Installed management cluster %s (%s)", opts.Name, since(start))
	return nil
}

// ensureInstaller returns the openshift-install binary for the release from
// the cache, extracting it from the release image if needed.
func ensureInstaller(ctx context.Context, p progress, dirs xdg.Dirs, release ocp.Release, pullSecret string) (ocp.Installer, error) {
	cacheDir := filepath.Join(dirs.Cache, "releases")
	if installer, found := ocp.New().CachedInstaller(cacheDir, release); found {
		p.done("Using cached openshift-install %s", release.Name)
		return installer, nil
	}
	var installer ocp.Installer
	start := time.Now()
	err := p.wait(ctx, "Extracting openshift-install "+release.Name+" from "+release.PullSpec, func(ctx context.Context) error {
		var err error
		installer, err = ocp.New().ExtractInstaller(ctx, cacheDir, release, pullSecret)
		return err
	})
	if err != nil {
		return ocp.Installer{}, err
	}
	p.done("Extracted openshift-install %s (%s)", release.Name, since(start))
	return installer, nil
}

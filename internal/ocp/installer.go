package ocp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Installer is an openshift-install binary extracted from a release payload.
type Installer struct {
	Path string
}

// CachedInstaller returns the openshift-install binary for the release if it
// is in cacheDir.
func CachedInstaller(cacheDir string, release Release) (Installer, bool) {
	installer := Installer{Path: filepath.Join(cacheDir, release.Name, "openshift-install")}
	if _, err := os.Stat(installer.Path); err != nil {
		return Installer{}, false
	}
	return installer, true
}

// ExtractInstaller extracts the openshift-install binary for the release into
// cacheDir.
func ExtractInstaller(ctx context.Context, cacheDir string, release Release, pullSecretPath string) (Installer, error) {
	// Extract into a temporary directory so an interrupted extraction never
	// leaves a broken binary in the cache.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return Installer{}, err
	}
	tmpDir, err := os.MkdirTemp(cacheDir, release.Name+".tmp-")
	if err != nil {
		return Installer{}, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.CommandContext(ctx, "oc", "adm", "release", "extract",
		"--command=openshift-install",
		"--to="+tmpDir,
		"--registry-config="+pullSecretPath,
		release.PullSpec,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return Installer{}, fmt.Errorf("extracting openshift-install from %s: %w\n%s", release.PullSpec, err, out)
	}
	dir := filepath.Join(cacheDir, release.Name)
	if err := os.Rename(tmpDir, dir); err != nil {
		return Installer{}, err
	}
	return Installer{Path: filepath.Join(dir, "openshift-install")}, nil
}

// CreateCluster runs "openshift-install create cluster" in installDir,
// writing the installer output to out.
func (i Installer) CreateCluster(ctx context.Context, installDir string, release Release, out io.Writer) error {
	cmd := exec.CommandContext(ctx, i.Path, "create", "cluster", "--dir", installDir)
	cmd.Env = append(os.Environ(),
		"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE="+release.PullSpec,
		"OPENSHIFT_INSTALL_EXPERIMENTAL_DISABLE_IMAGE_POLICY=true",
	)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("openshift-install create cluster: %w", err)
	}
	return nil
}

// DestroyCluster runs "openshift-install destroy cluster" in installDir,
// writing the installer output to out.
func (i Installer) DestroyCluster(ctx context.Context, installDir string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, i.Path, "destroy", "cluster", "--dir", installDir)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("openshift-install destroy cluster: %w", err)
	}
	return nil
}

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

// EnsureInstaller returns the openshift-install binary for the release,
// extracting it into cacheDir unless it is already cached.
func EnsureInstaller(ctx context.Context, cacheDir string, release Release, pullSecretPath string) (Installer, error) {
	dir := filepath.Join(cacheDir, release.Version)
	installer := Installer{Path: filepath.Join(dir, "openshift-install")}
	if _, err := os.Stat(installer.Path); err == nil {
		return installer, nil
	}

	// Extract into a temporary directory so an interrupted extraction never
	// leaves a broken binary in the cache.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return Installer{}, err
	}
	tmpDir, err := os.MkdirTemp(cacheDir, release.Version+".tmp-")
	if err != nil {
		return Installer{}, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.CommandContext(ctx, "oc", "adm", "release", "extract",
		"--command=openshift-install",
		"--to="+tmpDir,
		"--registry-config="+pullSecretPath,
		release.PullSpec(),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return Installer{}, fmt.Errorf("extracting openshift-install from %s: %w\n%s", release.PullSpec(), err, out)
	}
	if err := os.Rename(tmpDir, dir); err != nil {
		return Installer{}, err
	}
	return installer, nil
}

// CreateCluster runs "openshift-install create cluster" in installDir.
func (i Installer) CreateCluster(ctx context.Context, installDir string, release Release, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, i.Path, "create", "cluster", "--dir", installDir)
	cmd.Env = append(os.Environ(),
		"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE="+release.PullSpec(),
		"OPENSHIFT_INSTALL_EXPERIMENTAL_DISABLE_IMAGE_POLICY=true",
	)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("openshift-install create cluster: %w", err)
	}
	return nil
}

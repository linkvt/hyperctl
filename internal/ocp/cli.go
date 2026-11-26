package ocp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/linkvt/hyperctl/internal/process"
)

// CLI runs oc against release images and the openshift-install binaries
// extracted from them.
type CLI struct {
	runner process.Runner
}

// Installer is an openshift-install binary extracted from a release payload.
type Installer struct {
	Path string
}

// New returns a CLI running the oc binary in PATH.
func New() CLI {
	return CLI{runner: process.Exec{}}
}

// CheckPullable verifies that the release image can be read with the pull
// secret.
func (c CLI) CheckPullable(ctx context.Context, release Release, pullSecretPath string) error {
	var out bytes.Buffer
	err := c.runner.Run(ctx, process.Command{
		Path: "oc",
		Args: []string{"adm", "release", "info",
			"--registry-config=" + pullSecretPath,
			release.PullSpec,
		},
		Stdout: &out,
		Stderr: &out,
	})
	if err != nil {
		detail := strings.TrimSpace(out.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("the release image %s cannot be pulled with the pull secret %s:\n%s", release.PullSpec, pullSecretPath, detail)
	}
	return nil
}

// CachedInstaller returns the openshift-install binary for the release if it
// is in cacheDir.
func (c CLI) CachedInstaller(cacheDir string, release Release) (Installer, bool) {
	installer := Installer{Path: filepath.Join(cacheDir, release.Name, "openshift-install")}
	if _, err := os.Stat(installer.Path); err != nil {
		return Installer{}, false
	}
	return installer, true
}

// ExtractInstaller extracts the openshift-install binary for the release into
// cacheDir.
func (c CLI) ExtractInstaller(ctx context.Context, cacheDir string, release Release, pullSecretPath string) (Installer, error) {
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

	var out bytes.Buffer
	err = c.runner.Run(ctx, process.Command{
		Path: "oc",
		Args: []string{"adm", "release", "extract",
			"--command=openshift-install",
			"--to=" + tmpDir,
			"--registry-config=" + pullSecretPath,
			release.PullSpec,
		},
		Stdout: &out,
		Stderr: &out,
	})
	if err != nil {
		return Installer{}, fmt.Errorf("extracting openshift-install from %s: %w\n%s", release.PullSpec, err, out.Bytes())
	}
	dir := filepath.Join(cacheDir, release.Name)
	if err := os.Rename(tmpDir, dir); err != nil {
		return Installer{}, err
	}
	return Installer{Path: filepath.Join(dir, "openshift-install")}, nil
}

// CreateCluster runs "openshift-install create cluster" in installDir,
// writing the installer output to out.
func (c CLI) CreateCluster(ctx context.Context, installer Installer, installDir string, release Release, out io.Writer) error {
	err := c.runner.Run(ctx, process.Command{
		Path: installer.Path,
		Args: []string{"create", "cluster", "--dir", installDir},
		Env: []string{
			"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE=" + release.PullSpec,
			"OPENSHIFT_INSTALL_EXPERIMENTAL_DISABLE_IMAGE_POLICY=true",
		},
		Stdout: out,
		Stderr: out,
	})
	if err != nil {
		return fmt.Errorf("openshift-install create cluster: %w", err)
	}
	return nil
}

// DestroyCluster runs "openshift-install destroy cluster" in installDir,
// writing the installer output to out.
func (c CLI) DestroyCluster(ctx context.Context, installer Installer, installDir string, out io.Writer) error {
	err := c.runner.Run(ctx, process.Command{
		Path:   installer.Path,
		Args:   []string{"destroy", "cluster", "--dir", installDir},
		Stdout: out,
		Stderr: out,
	})
	if err != nil {
		return fmt.Errorf("openshift-install destroy cluster: %w", err)
	}
	return nil
}

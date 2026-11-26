package app_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/linkvt/hyperctl/internal/ocp"
)

const testMgmtKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: fake-mgmt
  cluster:
    server: https://api.mgmt.fake:6443
contexts:
- name: admin
  context:
    cluster: fake-mgmt
    user: admin
current-context: admin
users:
- name: admin
  user:
    token: mgmt-token
`

// fakeReleaseImages provides release images and the openshift-install
// binaries extracted from them. Extracted installers are files in the cache
// directory, so that removing the cache removes them.
type fakeReleaseImages struct {
	// pullErr fails the pull check of every release image.
	pullErr    error
	pullChecks []pullCheck
	// extractions counts the extracted installers.
	extractions int
	// installErr fails the installation after the installer wrote its
	// files, like an installation that times out.
	installErr error
	creates    []installerCall
	destroys   []installerCall
}

// pullCheck is a release image pull check.
type pullCheck struct {
	release    ocp.Release
	pullSecret string
}

// installerCall is a run of openshift-install.
type installerCall struct {
	installDir string
	release    ocp.Release
	// installConfig is the install-config.yaml the installer consumed.
	installConfig string
}

func (f *fakeReleaseImages) CheckPullable(_ context.Context, release ocp.Release, pullSecretPath string) error {
	f.pullChecks = append(f.pullChecks, pullCheck{release: release, pullSecret: pullSecretPath})
	return f.pullErr
}

func (f *fakeReleaseImages) CachedInstaller(cacheDir string, release ocp.Release) (ocp.Installer, bool) {
	installer := fakeInstallerPath(cacheDir, release)
	if _, err := os.Stat(installer.Path); err != nil {
		return ocp.Installer{}, false
	}
	return installer, true
}

func (f *fakeReleaseImages) ExtractInstaller(_ context.Context, cacheDir string, release ocp.Release, _ string) (ocp.Installer, error) {
	installer := fakeInstallerPath(cacheDir, release)
	if err := os.MkdirAll(filepath.Dir(installer.Path), 0o755); err != nil {
		return ocp.Installer{}, err
	}
	if err := os.WriteFile(installer.Path, nil, 0o600); err != nil {
		return ocp.Installer{}, err
	}
	f.extractions++
	return installer, nil
}

// CreateCluster consumes the install-config and writes the files the real
// installer leaves behind.
func (f *fakeReleaseImages) CreateCluster(_ context.Context, installer ocp.Installer, installDir string, release ocp.Release, out io.Writer) error {
	installConfigPath := filepath.Join(installDir, "install-config.yaml")
	installConfig, err := os.ReadFile(installConfigPath)
	if err != nil {
		return err
	}
	if err := os.Remove(installConfigPath); err != nil {
		return err
	}
	f.creates = append(f.creates, installerCall{installDir: installDir, release: release, installConfig: string(installConfig)})
	_, _ = fmt.Fprintln(out, "level=info msg=fake install")
	if err := os.MkdirAll(filepath.Join(installDir, "auth"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(installDir, "auth", "kubeconfig"), []byte(testMgmtKubeconfig), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(installDir, "metadata.json"), []byte("{}\n"), 0o600); err != nil {
		return err
	}
	return f.installErr
}

func (f *fakeReleaseImages) DestroyCluster(_ context.Context, installer ocp.Installer, installDir string, _ io.Writer) error {
	f.destroys = append(f.destroys, installerCall{installDir: installDir})
	return nil
}

func fakeInstallerPath(cacheDir string, release ocp.Release) ocp.Installer {
	return ocp.Installer{Path: filepath.Join(cacheDir, release.Name, "openshift-install")}
}

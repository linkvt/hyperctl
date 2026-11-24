package app

import (
	"path/filepath"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// clusterDir holds everything hyperctl keeps for a management cluster.
func clusterDir(dirs xdg.Dirs, name string) string {
	return filepath.Join(dirs.Data, "clusters", name)
}

// installDir is the openshift-install asset directory, needed for destroy.
func installDir(dirs xdg.Dirs, name string) string {
	return filepath.Join(clusterDir(dirs, name), "install")
}

// mgmtKubeconfigPath is where the installer writes the admin kubeconfig. For
// AKS clusters hyperctl writes it to the same place.
func mgmtKubeconfigPath(dirs xdg.Dirs, name string) string {
	return filepath.Join(installDir(dirs, name), "auth", "kubeconfig")
}

// hostedDir holds everything hyperctl keeps for a hosted cluster.
func hostedDir(dirs xdg.Dirs, mgmt, hosted string) string {
	return filepath.Join(clusterDir(dirs, mgmt), "hosted", hosted)
}

func hostedKubeconfigPath(dirs xdg.Dirs, mgmt, hosted string) string {
	return filepath.Join(hostedDir(dirs, mgmt, hosted), "kubeconfig")
}

// azureDir holds the credentials and keys of the shared Azure resources.
func azureDir(dirs xdg.Dirs) string {
	return filepath.Join(dirs.Data, "azure")
}

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

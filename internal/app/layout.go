package app

import (
	"path/filepath"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// azureDir holds the credentials and keys of the shared Azure resources.
func azureDir(dirs xdg.Dirs) string {
	return filepath.Join(dirs.Data, "azure")
}

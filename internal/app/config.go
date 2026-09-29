package app

import (
	"path/filepath"
)

// config holds user defaults, edited by the user or saved from prompts.
type config struct {
	Region     string `json:"region,omitempty"`
	BaseDomain string `json:"baseDomain,omitempty"`
	PullSecret string `json:"pullSecret,omitempty"`
	// SSHKey is the path of the SSH public key for node access. nil means not
	// configured yet, an empty string means no SSH key.
	SSHKey     *string `json:"sshKey,omitempty"`
	OIDCBucket string  `json:"oidcBucket,omitempty"`
	// MgmtInstanceType and MgmtWorkers size the management cluster.
	MgmtInstanceType string `json:"mgmtInstanceType,omitempty"`
	MgmtWorkers      *int   `json:"mgmtWorkers,omitempty"`
	// Hypershift is the path of the hypershift binary.
	Hypershift string `json:"hypershift,omitempty"`
}

func loadConfig(configDir string) (config, error) {
	var c config
	_, err := readYAML(filepath.Join(configDir, "config.yaml"), &c)
	return c, err
}

func saveConfig(configDir string, c config) error {
	return writeYAML(filepath.Join(configDir, "config.yaml"), c)
}

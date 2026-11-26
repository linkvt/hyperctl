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
	// Azure holds the defaults of Azure clusters, the values above are for
	// AWS clusters.
	Azure azureConfig `json:"azure,omitzero"`
}

// azureConfig holds user defaults for Azure clusters.
type azureConfig struct {
	// Location is the default location, before the last used one.
	Location   string `json:"location,omitempty"`
	BaseDomain string `json:"baseDomain,omitempty"`
	// BaseDomainResourceGroup holds the public DNS zone of the base domain.
	BaseDomainResourceGroup string `json:"baseDomainResourceGroup,omitempty"`
	// ResourceGroup holds the resources shared by all clusters: the OIDC
	// issuer, the External DNS zone and the workload identities.
	ResourceGroup      string `json:"resourceGroup,omitempty"`
	OIDCStorageAccount string `json:"oidcStorageAccount,omitempty"`
	// DNSZone is the External DNS zone, a subdomain of the base domain.
	DNSZone string `json:"dnsZone,omitempty"`
	// MgmtVMSize and MgmtNodes size the AKS management cluster.
	MgmtVMSize string `json:"mgmtVMSize,omitempty"`
	MgmtNodes  int    `json:"mgmtNodes,omitempty"`
}

func loadConfig(configDir string) (config, error) {
	var c config
	_, err := readYAML(filepath.Join(configDir, "config.yaml"), &c)
	return c, err
}

func saveConfig(configDir string, c config) error {
	return writeYAML(filepath.Join(configDir, "config.yaml"), c)
}

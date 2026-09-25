package ocp

import "sigs.k8s.io/yaml"

// InstallConfigParams are the values hyperctl sets in install-config.yaml.
// Everything else is left to the installer defaults.
type InstallConfigParams struct {
	Name       string
	BaseDomain string
	Region     string
	PullSecret string
	// SSHKey is an optional public key for debugging access to the nodes.
	SSHKey string
}

type installConfig struct {
	APIVersion string                `json:"apiVersion"`
	BaseDomain string                `json:"baseDomain"`
	Metadata   installConfigMetadata `json:"metadata"`
	Platform   installConfigPlatform `json:"platform"`
	PullSecret string                `json:"pullSecret"`
	SSHKey     string                `json:"sshKey,omitempty"`
}

type installConfigMetadata struct {
	Name string `json:"name"`
}

type installConfigPlatform struct {
	AWS installConfigAWS `json:"aws"`
}

type installConfigAWS struct {
	Region string `json:"region"`
}

// RenderInstallConfig renders an AWS install-config.yaml.
func RenderInstallConfig(p InstallConfigParams) ([]byte, error) {
	return yaml.Marshal(installConfig{
		APIVersion: "v1",
		BaseDomain: p.BaseDomain,
		Metadata:   installConfigMetadata{Name: p.Name},
		Platform:   installConfigPlatform{AWS: installConfigAWS{Region: p.Region}},
		PullSecret: p.PullSecret,
		SSHKey:     p.SSHKey,
	})
}

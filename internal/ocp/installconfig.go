package ocp

import "sigs.k8s.io/yaml"

// controlPlaneReplicas is the number of control plane nodes; single-node
// clusters are not supported as HyperShift management clusters.
const controlPlaneReplicas = 3

// InstallConfigParams are the values hyperctl sets in install-config.yaml.
// Everything else is left to the installer defaults.
type InstallConfigParams struct {
	Name       string
	BaseDomain string
	Region     string
	// Zone restricts all machines to one availability zone. Empty means the
	// installer spreads them across the zones of the region.
	Zone string
	// InstanceType is the EC2 instance type of all machines.
	InstanceType string
	// Workers is the number of compute nodes. With zero, the control plane
	// nodes run workloads too.
	Workers    int
	PullSecret string
	// SSHKey is an optional public key for debugging access to the nodes.
	SSHKey string
}

type installConfig struct {
	APIVersion   string                `json:"apiVersion"`
	BaseDomain   string                `json:"baseDomain"`
	Compute      []installConfigPool   `json:"compute"`
	ControlPlane installConfigPool     `json:"controlPlane"`
	Metadata     installConfigMetadata `json:"metadata"`
	Platform     installConfigPlatform `json:"platform"`
	PullSecret   string                `json:"pullSecret"`
	SSHKey       string                `json:"sshKey,omitempty"`
}

type installConfigPool struct {
	Name     string                    `json:"name"`
	Platform installConfigPoolPlatform `json:"platform"`
	Replicas int                       `json:"replicas"`
}

type installConfigPoolPlatform struct {
	AWS installConfigPoolAWS `json:"aws"`
}

type installConfigPoolAWS struct {
	Type string `json:"type"`
}

type installConfigMetadata struct {
	Name string `json:"name"`
}

type installConfigPlatform struct {
	AWS installConfigAWS `json:"aws"`
}

type installConfigAWS struct {
	DefaultMachinePlatform *installConfigMachinePlatform `json:"defaultMachinePlatform,omitempty"`
	Region                 string                        `json:"region"`
}

type installConfigMachinePlatform struct {
	Zones []string `json:"zones"`
}

// RenderInstallConfig renders an AWS install-config.yaml.
func RenderInstallConfig(p InstallConfigParams) ([]byte, error) {
	poolPlatform := installConfigPoolPlatform{AWS: installConfigPoolAWS{Type: p.InstanceType}}
	platform := installConfigAWS{Region: p.Region}
	if p.Zone != "" {
		platform.DefaultMachinePlatform = &installConfigMachinePlatform{Zones: []string{p.Zone}}
	}
	return yaml.Marshal(installConfig{
		APIVersion:   "v1",
		BaseDomain:   p.BaseDomain,
		Compute:      []installConfigPool{{Name: "worker", Platform: poolPlatform, Replicas: p.Workers}},
		ControlPlane: installConfigPool{Name: "master", Platform: poolPlatform, Replicas: controlPlaneReplicas},
		Metadata:     installConfigMetadata{Name: p.Name},
		Platform:     installConfigPlatform{AWS: platform},
		PullSecret:   p.PullSecret,
		SSHKey:       p.SSHKey,
	})
}

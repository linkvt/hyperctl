package hypershift

import "context"

// InstallAzureParams configure the HyperShift operator installation on an
// AKS management cluster.
type InstallAzureParams struct {
	PullSecret string
	// ExternalDNSCredentialsFile is the service principal file External DNS
	// uses to manage the records of ExternalDNSDomain.
	ExternalDNSCredentialsFile string
	ExternalDNSDomain          string
}

// InstallAzure installs the HyperShift operator with External DNS and the
// standalone karpenter-operator, which Azure AutoNode requires, and waits
// until it is available.
func (c CLI) InstallAzure(ctx context.Context, p InstallAzureParams) error {
	return c.run(ctx, "install",
		"--pull-secret", p.PullSecret,
		"--limit-crd-install", "Azure",
		"--tech-preview-no-upgrade",
		"--enable-standalone-karpenter-operator",
		"--external-dns-provider", "azure",
		"--external-dns-credentials", p.ExternalDNSCredentialsFile,
		"--external-dns-domain-filter", p.ExternalDNSDomain,
		"--wait-until-available",
	)
}

// CreateAzureIAMParams configure the workload identities of a hosted cluster.
type CreateAzureIAMParams struct {
	Name            string
	InfraID         string
	CredentialsFile string
	Location        string
	// ResourceGroup holds the identities.
	ResourceGroup string
	OIDCIssuerURL string
	// OutputFile receives the workload identities.
	OutputFile string
}

// CreateAzureIAM creates the workload identities of a hosted cluster,
// including the one of Karpenter.
func (c CLI) CreateAzureIAM(ctx context.Context, p CreateAzureIAMParams) error {
	return c.run(ctx, "create", "iam", "azure",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--azure-creds", p.CredentialsFile,
		"--location", p.Location,
		"--resource-group-name", p.ResourceGroup,
		"--oidc-issuer-url", p.OIDCIssuerURL,
		"--output-file", p.OutputFile,
		"--enable-karpenter",
	)
}

// CreateAzureInfraParams configure the infrastructure of a hosted cluster.
type CreateAzureInfraParams struct {
	Name                   string
	InfraID                string
	CredentialsFile        string
	BaseDomain             string
	Location               string
	WorkloadIdentitiesFile string
	// DNSZoneResourceGroup holds the public DNS zone of the base domain.
	DNSZoneResourceGroup string
	// OutputFile receives the infrastructure description.
	OutputFile string
}

// CreateAzureInfra creates the resource groups, network and DNS of a hosted
// cluster. It assigns no roles, see the role assignments of hyperctl.
func (c CLI) CreateAzureInfra(ctx context.Context, p CreateAzureInfraParams) error {
	return c.run(ctx, "create", "infra", "azure",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--azure-creds", p.CredentialsFile,
		"--base-domain", p.BaseDomain,
		"--location", p.Location,
		"--workload-identities-file", p.WorkloadIdentitiesFile,
		"--dns-zone-rg-name", p.DNSZoneResourceGroup,
		"--output-file", p.OutputFile,
	)
}

// CreateAzureClusterParams configure an Azure hosted cluster.
type CreateAzureClusterParams struct {
	Name                 string
	InfraID              string
	CredentialsFile      string
	Location             string
	BaseDomain           string
	DNSZoneResourceGroup string
	PullSecret           string
	ReleaseImage         string
	// SSHKey is an optional path to an SSH public key, hypershift generates
	// one without it.
	SSHKey string
	// OIDCIssuerURL and SigningKeyFile are the issuer that Azure trusts and
	// its private key, which signs the service account tokens.
	OIDCIssuerURL  string
	SigningKeyFile string
	InfraFile      string
	// ExternalDNSDomain holds the records of the control plane endpoints.
	ExternalDNSDomain string
}

// podSecurityOverride lets the control plane pods run on AKS, which enforces
// the restricted pod security level by default.
const podSecurityOverride = "hypershift.openshift.io/pod-security-admission-label-override=baseline"

// CreateAzureCluster creates an Azure hosted cluster with AutoNode on an AKS
// management cluster and waits until it is available.
func (c CLI) CreateAzureCluster(ctx context.Context, p CreateAzureClusterParams) error {
	args := []string{"create", "cluster", "azure",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--azure-creds", p.CredentialsFile,
		"--location", p.Location,
		"--base-domain", p.BaseDomain,
		"--dns-zone-rg-name", p.DNSZoneResourceGroup,
		"--pull-secret", p.PullSecret,
		"--release-image", p.ReleaseImage,
		"--node-pool-replicas", "2",
		"--oidc-issuer-url", p.OIDCIssuerURL,
		"--sa-token-issuer-private-key-path", p.SigningKeyFile,
		"--infra-json", p.InfraFile,
		"--diagnostics-storage-account-type", "Managed",
		"--external-dns-domain", p.ExternalDNSDomain,
		"--annotations", podSecurityOverride,
		"--auto-node",
		"--wait",
		"--timeout", createClusterTimeout.String(),
	}
	if p.SSHKey != "" {
		args = append(args, "--ssh-key", p.SSHKey)
	} else {
		args = append(args, "--generate-ssh")
	}
	return c.run(ctx, args...)
}

// DestroyAzureParams identify an Azure hosted cluster to destroy.
type DestroyAzureParams struct {
	Name            string
	InfraID         string
	CredentialsFile string
	Location        string
	// DNSZoneResourceGroup holds the public DNS zone of the base domain. Only
	// DestroyAzureCluster uses it.
	DNSZoneResourceGroup string
}

// DestroyAzureCluster deletes an Azure hosted cluster and then its
// infrastructure, see DestroyAzureInfra.
func (c CLI) DestroyAzureCluster(ctx context.Context, p DestroyAzureParams) error {
	return c.run(ctx, "destroy", "cluster", "azure",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--azure-creds", p.CredentialsFile,
		"--location", p.Location,
		"--dns-zone-rg-name", p.DNSZoneResourceGroup,
	)
}

// DestroyAzureInfra deletes the resource groups of a hosted cluster by its
// infra ID. Unlike DestroyAzureCluster, it works without the management
// cluster.
func (c CLI) DestroyAzureInfra(ctx context.Context, p DestroyAzureParams) error {
	return c.run(ctx, "destroy", "infra", "azure",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--azure-creds", p.CredentialsFile,
		"--location", p.Location,
	)
}

// DestroyAzureIAMParams identify the workload identities of a hosted cluster.
type DestroyAzureIAMParams struct {
	Name            string
	InfraID         string
	CredentialsFile string
	// ResourceGroup holds the identities.
	ResourceGroup string
	// WorkloadIdentitiesFile is the output of CreateAzureIAM. hypershift
	// only validates it, it finds the identities by their names.
	WorkloadIdentitiesFile string
	// DNSZoneResourceGroup holds the public DNS zone of the base domain.
	DNSZoneResourceGroup string
}

// DestroyAzureIAM deletes the workload identities of a hosted cluster and
// their federated credentials.
func (c CLI) DestroyAzureIAM(ctx context.Context, p DestroyAzureIAMParams) error {
	return c.run(ctx, "destroy", "iam", "azure",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--azure-creds", p.CredentialsFile,
		"--resource-group-name", p.ResourceGroup,
		"--workload-identities-file", p.WorkloadIdentitiesFile,
		"--dns-zone-rg-name", p.DNSZoneResourceGroup,
	)
}

// Package hypershift runs the hypershift CLI against a management cluster.
package hypershift

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CLI runs a hypershift binary against the management cluster of Kubeconfig.
type CLI struct {
	Path       string
	Kubeconfig string
	// Output receives the output of hypershift.
	Output io.Writer
}

// InstallParams configure the HyperShift operator installation.
type InstallParams struct {
	OIDCBucket         string
	OIDCBucketRegion   string
	AWSCredentialsFile string
	// StandaloneKarpenterOperator deploys the standalone karpenter-operator
	// instead of the one embedded in the HyperShift operator image.
	StandaloneKarpenterOperator bool
}

// Install installs the HyperShift operator and waits until it is available.
// Installing is idempotent, so it is safe to run on every create.
func (c CLI) Install(ctx context.Context, p InstallParams) error {
	args := []string{"install",
		"--oidc-storage-provider-s3-bucket-name", p.OIDCBucket,
		"--oidc-storage-provider-s3-credentials", p.AWSCredentialsFile,
		"--oidc-storage-provider-s3-region", p.OIDCBucketRegion,
		"--tech-preview-no-upgrade",
	}
	if p.StandaloneKarpenterOperator {
		args = append(args, "--enable-standalone-karpenter-operator")
	}
	return c.run(ctx, append(args, "--wait-until-available")...)
}

// CreateClusterParams configure an AWS hosted cluster.
type CreateClusterParams struct {
	Name string
	// InfraID tags the AWS resources of the cluster, see NewInfraID.
	InfraID      string
	Region       string
	BaseDomain   string
	PullSecret   string
	ReleaseImage string
	// SSHKey is an optional path to an SSH public key.
	SSHKey string
}

// createClusterTimeout bounds waiting for the hosted cluster to become
// available.
const createClusterTimeout = 45 * time.Minute

// CreateCluster creates an AWS hosted cluster with AutoNode and waits until
// it is available.
func (c CLI) CreateCluster(ctx context.Context, p CreateClusterParams) error {
	args := []string{"create", "cluster", "aws",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--region", p.Region,
		"--base-domain", p.BaseDomain,
		"--pull-secret", p.PullSecret,
		"--release-image", p.ReleaseImage,
		"--node-pool-replicas", "1",
		"--auto-node",
		"--wait",
		"--timeout", createClusterTimeout.String(),
	}
	if p.SSHKey != "" {
		args = append(args, "--ssh-key", p.SSHKey)
	}
	return c.run(ctx, args...)
}

// DestroyClusterParams identify an AWS hosted cluster to destroy.
type DestroyClusterParams struct {
	Name       string
	InfraID    string
	Region     string
	BaseDomain string
}

// DestroyCluster destroys an AWS hosted cluster and its cloud resources.
func (c CLI) DestroyCluster(ctx context.Context, p DestroyClusterParams) error {
	return c.run(ctx, "destroy", "cluster", "aws",
		"--name", p.Name,
		"--infra-id", p.InfraID,
		"--region", p.Region,
		"--base-domain", p.BaseDomain,
	)
}

// DestroyInfra destroys the AWS infrastructure and IAM resources of a hosted
// cluster by its infra ID. Unlike DestroyCluster, it works without the
// management cluster.
func (c CLI) DestroyInfra(ctx context.Context, p DestroyClusterParams) error {
	if err := c.run(ctx, "destroy", "infra", "aws",
		"--infra-id", p.InfraID,
		"--name", p.Name,
		"--region", p.Region,
		"--base-domain", p.BaseDomain,
	); err != nil {
		return err
	}
	return c.run(ctx, "destroy", "iam", "aws",
		"--infra-id", p.InfraID,
		"--region", p.Region,
	)
}

// HostedKubeconfig returns an admin kubeconfig for the hosted cluster.
func (c CLI) HostedKubeconfig(ctx context.Context, name string) ([]byte, error) {
	cmd := c.command(ctx, "create", "kubeconfig", "--name", name)
	cmd.Stderr = c.Output
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("hypershift create kubeconfig: %w", err)
	}
	return out, nil
}

func (c CLI) run(ctx context.Context, args ...string) error {
	cmd := c.command(ctx, args...)
	cmd.Stdout = c.Output
	cmd.Stderr = c.Output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hypershift %s: %w", strings.Join(args[:2], " "), err)
	}
	return nil
}

// command returns a hypershift command running against the management cluster.
// Not all hypershift subcommands have a --kubeconfig flag, so KUBECONFIG is
// set for all of them.
func (c CLI) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+c.Kubeconfig)
	return cmd
}

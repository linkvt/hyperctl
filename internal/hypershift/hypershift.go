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
	Stdout     io.Writer
	Stderr     io.Writer
}

// InstallParams configure the HyperShift operator installation.
type InstallParams struct {
	OIDCBucket         string
	OIDCBucketRegion   string
	AWSCredentialsFile string
}

// Install installs the HyperShift operator and waits until it is available.
// Installing is idempotent, so it is safe to run on every create.
func (c CLI) Install(ctx context.Context, p InstallParams) error {
	return c.run(ctx, "install",
		"--oidc-storage-provider-s3-bucket-name", p.OIDCBucket,
		"--oidc-storage-provider-s3-credentials", p.AWSCredentialsFile,
		"--oidc-storage-provider-s3-region", p.OIDCBucketRegion,
		"--tech-preview-no-upgrade",
		"--wait-until-available",
	)
}

// CreateClusterParams configure an AWS hosted cluster.
type CreateClusterParams struct {
	Name         string
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

// DestroyCluster destroys an AWS hosted cluster and its cloud resources.
func (c CLI) DestroyCluster(ctx context.Context, name, region, baseDomain string) error {
	return c.run(ctx, "destroy", "cluster", "aws",
		"--name", name,
		"--region", region,
		"--base-domain", baseDomain,
	)
}

// HostedKubeconfig returns an admin kubeconfig for the hosted cluster.
func (c CLI) HostedKubeconfig(ctx context.Context, name string) ([]byte, error) {
	cmd := c.command(ctx, "create", "kubeconfig", "--name", name)
	cmd.Stderr = c.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("hypershift create kubeconfig: %w", err)
	}
	return out, nil
}

func (c CLI) run(ctx context.Context, args ...string) error {
	cmd := c.command(ctx, args...)
	cmd.Stdout = c.Stdout
	cmd.Stderr = c.Stderr
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

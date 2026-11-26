// Package hypershift runs the hypershift CLI against a management cluster.
package hypershift

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/linkvt/hyperctl/internal/process"
)

// CLI runs a hypershift binary against a management cluster.
type CLI struct {
	path       string
	kubeconfig string
	output     io.Writer
	runner     process.Runner
}

// New returns the hypershift binary at path running against the management
// cluster of kubeconfig. Output receives the output of hypershift.
func New(path, kubeconfig string, output io.Writer) CLI {
	return CLI{path: path, kubeconfig: kubeconfig, output: output, runner: process.Exec{}}
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
	var out bytes.Buffer
	if err := c.runner.Run(ctx, c.command(&out, c.output, "create", "kubeconfig", "--name", name)); err != nil {
		return nil, fmt.Errorf("hypershift create kubeconfig: %w", err)
	}
	return out.Bytes(), nil
}

// Version checks that the binary runs by printing its version.
func (c CLI) Version(ctx context.Context) error {
	var out bytes.Buffer
	cmd := process.Command{Path: c.path, Args: []string{"--version"}, Stdout: &out, Stderr: &out}
	if err := c.runner.Run(ctx, cmd); err != nil {
		return fmt.Errorf("running %s --version: %w\n%s", c.path, err, out.Bytes())
	}
	return nil
}

func (c CLI) run(ctx context.Context, args ...string) error {
	if err := c.runner.Run(ctx, c.command(c.output, c.output, args...)); err != nil {
		return fmt.Errorf("hypershift %s: %w", strings.Join(args[:2], " "), err)
	}
	return nil
}

// command returns a hypershift command running against the management cluster.
// Not all hypershift subcommands have a --kubeconfig flag, so KUBECONFIG is
// set for all of them.
func (c CLI) command(stdout, stderr io.Writer, args ...string) process.Command {
	return process.Command{
		Path:   c.path,
		Args:   args,
		Env:    []string{"KUBECONFIG=" + c.kubeconfig},
		Stdout: stdout,
		Stderr: stderr,
	}
}

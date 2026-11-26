package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/xdg"
)

const defaultBaseDomain = "autoscaling-dev.devcluster.openshift.com"

// AWSAccount are the AWS account operations hyperctl uses.
type AWSAccount interface {
	// CallerIdentity returns the ARN of the credentials in use.
	CallerIdentity(ctx context.Context) (string, error)
	// SpotRoleExists reports whether the EC2 Spot service-linked role exists.
	SpotRoleExists(ctx context.Context) (bool, error)
	// BucketRegion returns the region of a bucket and whether it exists.
	BucketRegion(ctx context.Context, bucket string) (string, bool, error)
	// CreatePublicBucket creates a bucket with publicly readable objects.
	CreatePublicBucket(ctx context.Context, bucket, region string) error
	// FirstZone returns the first available availability zone of a region.
	FirstZone(ctx context.Context, region string) (string, error)
	// ClusterDNS describes the DNS entries of a cluster with the given name.
	ClusterDNS(ctx context.Context, baseDomain, name string) ([]string, error)
	// DeleteClusterDNS deletes the DNS entries of a cluster with the given name.
	DeleteClusterDNS(ctx context.Context, baseDomain, name string) error
}

// awsPlatform creates OpenShift management clusters with openshift-install
// and AWS hosted clusters.
type awsPlatform struct {
	deps     Deps
	dirs     xdg.Dirs
	clusters clusterStore
}

// awsCreate is a create run on AWS.
type awsCreate struct {
	awsPlatform
	st  state
	cfg config
	// yes deletes DNS leftovers without confirmation.
	yes        bool
	oidcBucket string
	size       openshiftSize
	// credentialsFile and oidcBucketRegion are found by checkAccount and
	// prepare.
	credentialsFile  string
	oidcBucketRegion string
}

func (a awsPlatform) newCreate(cmd *cli.Command, st state, cfg config) (platformCreate, error) {
	run := &awsCreate{
		awsPlatform: a,
		st:          st,
		cfg:         cfg,
		yes:         cmd.Bool("yes"),
		oidcBucket:  cfg.OIDCBucket,
		size:        openshiftSize{InstanceType: cmp.Or(cfg.MgmtInstanceType, defaultInstanceType), Workers: compactWorkerCount},
	}
	if user := os.Getenv("USER"); user != "" {
		run.oidcBucket = cmp.Or(run.oidcBucket, user+"-hypershift-oidc")
	}
	run.oidcBucket = flagOr(cmd, "oidc-bucket", run.oidcBucket)
	if cfg.MgmtWorkers != nil {
		run.size.Workers = *cfg.MgmtWorkers
	}
	run.size.InstanceType = flagOr(cmd, "instance-type", run.size.InstanceType)
	if cmd.IsSet("workers") {
		run.size.Workers = cmd.Int("workers")
	}
	return run, nil
}

func (r *awsCreate) defaultRegion() string {
	return cmp.Or(r.cfg.Region, r.st.LastRegions.AWS, r.deps.DefaultRegion)
}

func (r *awsCreate) defaultBaseDomain() string {
	return cmp.Or(r.cfg.BaseDomain, defaultBaseDomain)
}

func (r *awsCreate) regionPrompt() (string, string) {
	return "AWS region", ""
}

func (r *awsCreate) promptSize(ctx context.Context, cmd *cli.Command) error {
	if cmd.IsSet("instance-type") || cmd.IsSet("workers") {
		return nil
	}
	return promptOpenShiftSize(ctx, &r.size)
}

func (r *awsCreate) missing() []string {
	var missing []string
	if r.size.InstanceType == "" {
		missing = append(missing, "--instance-type")
	}
	if r.oidcBucket == "" {
		missing = append(missing, "--oidc-bucket")
	}
	return missing
}

func (r *awsCreate) describeSize() string {
	return r.size.describe()
}

func (r *awsCreate) summary() []string {
	return []string{"OIDC bucket:    " + r.oidcBucket}
}

func (r *awsCreate) rememberRegion(st *state, region string) {
	st.LastRegions.AWS = region
}

func (r *awsCreate) checkAccount(ctx context.Context, p progress) error {
	var identity string
	err := p.wait(ctx, "Checking the AWS identity", func(ctx context.Context) error {
		var err error
		identity, err = requireIAMUser(ctx, r.deps.AWS)
		return err
	})
	if err != nil {
		return err
	}
	p.done("AWS identity %s", identity)
	if r.credentialsFile, err = awsCredentialsFile(); err != nil {
		return err
	}
	p.done("AWS credentials file %s", r.credentialsFile)
	return nil
}

// requireIAMUser fails unless the AWS credentials belong to an IAM user.
// Cluster installation must not run with temporary SAML (assumed role)
// credentials, e.g. from within an rh-aws-saml-login shell.
func requireIAMUser(ctx context.Context, account AWSAccount) (string, error) {
	identity, err := account.CallerIdentity(ctx)
	if err != nil {
		return "", err
	}
	parsed, err := arn.Parse(identity)
	if err != nil {
		return "", fmt.Errorf("parsing AWS caller identity %q: %w", identity, err)
	}
	if strings.HasPrefix(parsed.Resource, "assumed-role/") {
		return "", fmt.Errorf("AWS credentials are an assumed role (%s), clusters must be created with IAM user credentials; leave any rh-aws-saml-login shell and unset AWS_* session variables", identity)
	}
	if !strings.HasPrefix(parsed.Resource, "user/") {
		return "", fmt.Errorf("AWS credentials %s are not an IAM user", identity)
	}
	return identity, nil
}

// awsCredentialsFile returns the AWS shared credentials file the HyperShift
// operator uses to write OIDC documents. The operator reads its default
// profile.
func awsCredentialsFile() (string, error) {
	path := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".aws", "credentials")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("AWS credentials file for the HyperShift operator: %w", err)
	}
	return path, nil
}

// prepare checks the AWS account prerequisites before anything is
// installed, so failures surface before the long-running installation, and
// deletes DNS leftovers of previous clusters with the same names.
func (r *awsCreate) prepare(ctx context.Context, p progress, opts createOptions) error {
	p.step("Checking the AWS account")
	var err error
	if r.oidcBucketRegion, err = ensureAWSPrerequisites(ctx, p, r.deps.AWS, r.oidcBucket, opts.Region); err != nil {
		return err
	}
	untracked, err := untrackedClusterNames(r.clusters, opts)
	if err != nil {
		return err
	}
	return deleteDNSLeftovers(ctx, p, r.deps, r.yes, opts.BaseDomain, untracked...)
}

// ensureAWSPrerequisites checks that the EC2 Spot service-linked role
// exists. It creates the OIDC bucket in region if missing and returns the
// region of the bucket.
func ensureAWSPrerequisites(ctx context.Context, p progress, account AWSAccount, bucket, region string) (string, error) {
	var exists bool
	err := p.wait(ctx, "Checking the EC2 Spot service-linked role", func(ctx context.Context) error {
		var err error
		exists, err = account.SpotRoleExists(ctx)
		return err
	})
	if err != nil {
		return "", err
	}
	if !exists {
		return "", errors.New("the EC2 Spot service-linked role AWSServiceRoleForEC2Spot is missing in the AWS account, create it with: aws iam create-service-linked-role --aws-service-name spot.amazonaws.com")
	}
	p.done("EC2 Spot service-linked role exists")

	var bucketRegion string
	var found bool
	err = p.wait(ctx, "Checking OIDC bucket "+bucket, func(ctx context.Context) error {
		var err error
		bucketRegion, found, err = account.BucketRegion(ctx, bucket)
		return err
	})
	if err != nil {
		return "", err
	}
	if found {
		p.done("OIDC bucket %s exists in %s", bucket, bucketRegion)
		return bucketRegion, nil
	}
	err = p.wait(ctx, "Creating OIDC bucket "+bucket+" in "+region, func(ctx context.Context) error {
		return account.CreatePublicBucket(ctx, bucket, region)
	})
	if err != nil {
		return "", err
	}
	p.done("Created OIDC bucket %s in %s", bucket, region)
	return region, nil
}

// untrackedClusterNames returns the names of the clusters to create that
// hyperctl has no record of. DNS entries with these names are leftovers.
func untrackedClusterNames(clusters clusterStore, opts createOptions) ([]string, error) {
	candidates := map[string]clusterRef{opts.Name: managementCluster(opts.Name)}
	if !opts.ManagementOnly {
		candidates[opts.HostedName] = hostedCluster(opts.Name, opts.HostedName)
	}
	var names []string
	for name, c := range candidates {
		_, found, err := clusters.load(c)
		if err != nil {
			return nil, err
		}
		if !found {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

func (r *awsCreate) createMgmt(ctx context.Context, p progress, opts createOptions) error {
	return ensureMgmtCluster(ctx, p, r.deps, r.dirs, r.clusters, opts, r.size)
}

func (r *awsCreate) installOperator(ctx context.Context, p progress, hs Hypershift, opts createOptions) error {
	start := time.Now()
	if err := hs.Install(ctx, hypershift.InstallParams{
		OIDCBucket:         r.oidcBucket,
		OIDCBucketRegion:   r.oidcBucketRegion,
		AWSCredentialsFile: r.credentialsFile,

		StandaloneKarpenterOperator: opts.StandaloneKarpenterOperator,
	}); err != nil {
		return err
	}
	p.done("Installed the HyperShift operator (%s)", since(start))
	return nil
}

func (r *awsCreate) createHosted(ctx context.Context, p progress, hs Hypershift, opts createOptions) error {
	return ensureHostedCluster(ctx, p, hs, r.clusters, opts)
}

func (a awsPlatform) destroyHosted(ctx context.Context, p progress, hs Hypershift, yes bool, mgmt, hosted string) error {
	return destroyHostedCluster(ctx, p, yes, a.deps, hs, a.clusters, mgmt, hosted)
}

func (a awsPlatform) destroyMgmt(ctx context.Context, p progress, yes bool, name string) error {
	return destroyMgmtCluster(ctx, p, yes, a.deps, a.dirs, a.clusters, name)
}

// destroyShared keeps the OIDC bucket, it is not deleted yet, and the EC2
// Spot service-linked role, which other users of the account share.
func (a awsPlatform) destroyShared(_ context.Context, p progress, _ bool) error {
	p.done("No shared AWS resources to destroy, the OIDC bucket and the EC2 Spot service-linked role are kept")
	return nil
}

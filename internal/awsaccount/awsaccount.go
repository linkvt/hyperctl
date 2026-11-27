// Package awsaccount implements the AWS account operations hyperctl needs on
// top of the AWS SDK.
package awsaccount

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	spotServiceLinkedRole = "AWSServiceRoleForEC2Spot"
	bucketWaitTimeout     = 2 * time.Minute
)

// Account runs operations against the AWS account of the given config.
type Account struct {
	sts     *sts.Client
	ec2     *ec2.Client
	iam     *iam.Client
	s3      *s3.Client
	route53 *route53.Client
}

// New returns an Account using the credentials of cfg.
func New(cfg aws.Config) *Account {
	return &Account{
		sts:     sts.NewFromConfig(cfg),
		ec2:     ec2.NewFromConfig(cfg),
		iam:     iam.NewFromConfig(cfg),
		s3:      s3.NewFromConfig(cfg),
		route53: route53.NewFromConfig(cfg),
	}
}

// CallerIdentity returns the ARN of the credentials in use.
func (a *Account) CallerIdentity(ctx context.Context) (string, error) {
	out, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("getting AWS caller identity: %w", err)
	}
	return aws.ToString(out.Arn), nil
}

// SpotRoleExists reports whether the EC2 Spot service-linked role exists.
func (a *Account) SpotRoleExists(ctx context.Context) (bool, error) {
	_, err := a.iam.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(spotServiceLinkedRole)})
	if _, ok := errors.AsType[*iamtypes.NoSuchEntityException](err); ok {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting role %s: %w", spotServiceLinkedRole, err)
	}
	return true, nil
}

// BucketRegion returns the region of a bucket and whether it exists.
func (a *Account) BucketRegion(ctx context.Context, bucket string) (string, bool, error) {
	region, err := manager.GetBucketRegion(ctx, a.s3, bucket)
	if _, ok := errors.AsType[manager.BucketNotFound](err); ok {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("getting region of bucket %s: %w", bucket, err)
	}
	return region, true, nil
}

// CreatePublicBucket creates a bucket whose objects are publicly readable,
// as needed for OIDC discovery documents.
func (a *Account) CreatePublicBucket(ctx context.Context, bucket, region string) error {
	inRegion := func(o *s3.Options) { o.Region = region }

	input := &s3.CreateBucketInput{Bucket: aws.String(bucket)}
	// us-east-1 is the default location and must not be set explicitly.
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	if _, err := a.s3.CreateBucket(ctx, input, inRegion); err != nil {
		return fmt.Errorf("creating bucket %s: %w", bucket, err)
	}
	if err := s3.NewBucketExistsWaiter(a.s3).Wait(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}, bucketWaitTimeout, func(o *s3.BucketExistsWaiterOptions) {
		o.ClientOptions = append(o.ClientOptions, inRegion)
	}); err != nil {
		return fmt.Errorf("waiting for bucket %s: %w", bucket, err)
	}

	// New buckets block public policies, so the block has to be removed
	// before the public policy can be put.
	if _, err := a.s3.DeletePublicAccessBlock(ctx, &s3.DeletePublicAccessBlockInput{Bucket: aws.String(bucket)}, inRegion); err != nil {
		return fmt.Errorf("removing public access block of bucket %s: %w", bucket, err)
	}
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`, bucket)
	if _, err := a.s3.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(bucket), Policy: aws.String(policy)}, inRegion); err != nil {
		return fmt.Errorf("putting public read policy on bucket %s: %w", bucket, err)
	}
	return nil
}

// BucketObjects returns the keys of the objects in a bucket in region.
func (a *Account) BucketObjects(ctx context.Context, bucket, region string) ([]string, error) {
	var keys []string
	pages := s3.NewListObjectsV2Paginator(a.s3, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx, func(o *s3.Options) { o.Region = region })
		if err != nil {
			return nil, fmt.Errorf("listing objects of bucket %s: %w", bucket, err)
		}
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
	}
	return keys, nil
}

// DeleteBucket deletes an empty bucket in region.
func (a *Account) DeleteBucket(ctx context.Context, bucket, region string) error {
	if _, err := a.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)}, func(o *s3.Options) { o.Region = region }); err != nil {
		return fmt.Errorf("deleting bucket %s: %w", bucket, err)
	}
	return nil
}

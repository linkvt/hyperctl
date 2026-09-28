package app

import (
	"context"
	"errors"
)

// ensureAWSPrerequisites checks the AWS account prerequisites before anything
// is installed, so failures surface before the long-running installation. It
// creates the OIDC bucket if missing and returns its region.
func ensureAWSPrerequisites(ctx context.Context, p progress, account AWSAccount, opts createOptions) (string, error) {
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

	var region string
	var found bool
	err = p.wait(ctx, "Checking OIDC bucket "+opts.OIDCBucket, func(ctx context.Context) error {
		var err error
		region, found, err = account.BucketRegion(ctx, opts.OIDCBucket)
		return err
	})
	if err != nil {
		return "", err
	}
	if found {
		p.done("OIDC bucket %s exists in %s", opts.OIDCBucket, region)
		return region, nil
	}
	err = p.wait(ctx, "Creating OIDC bucket "+opts.OIDCBucket+" in "+opts.Region, func(ctx context.Context) error {
		return account.CreatePublicBucket(ctx, opts.OIDCBucket, opts.Region)
	})
	if err != nil {
		return "", err
	}
	p.done("Created OIDC bucket %s in %s", opts.OIDCBucket, opts.Region)
	return opts.Region, nil
}

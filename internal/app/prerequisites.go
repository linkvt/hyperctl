package app

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// ensureAWSPrerequisites checks the AWS account prerequisites before anything
// is installed, so failures surface before the long-running installation. It
// creates the OIDC bucket if missing and returns its region.
func ensureAWSPrerequisites(ctx context.Context, w io.Writer, account AWSAccount, opts createOptions) (string, error) {
	exists, err := account.SpotRoleExists(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", errors.New("the EC2 Spot service-linked role AWSServiceRoleForEC2Spot is missing in the AWS account, create it with: aws iam create-service-linked-role --aws-service-name spot.amazonaws.com")
	}

	region, found, err := account.BucketRegion(ctx, opts.OIDCBucket)
	if err != nil {
		return "", err
	}
	if found {
		return region, nil
	}
	_, _ = fmt.Fprintf(w, "Creating OIDC bucket %s in %s\n", opts.OIDCBucket, opts.Region)
	if err := account.CreatePublicBucket(ctx, opts.OIDCBucket, opts.Region); err != nil {
		return "", err
	}
	return opts.Region, nil
}

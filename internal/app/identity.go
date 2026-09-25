package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// requireIAMUser fails unless the AWS credentials belong to an IAM user.
// Cluster installation must not run with temporary SAML (assumed role)
// credentials, e.g. from within an rh-aws-saml-login shell.
func requireIAMUser(ctx context.Context, client STSClient) (string, error) {
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("getting AWS caller identity: %w", err)
	}
	identity := *out.Arn
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

package awsaccount

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// FirstZone returns the first available availability zone of a region, the
// zone hypershift uses for hosted clusters without explicit zones.
func (a *Account) FirstZone(ctx context.Context, region string) (string, error) {
	out, err := a.ec2.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []ec2types.Filter{{Name: aws.String("state"), Values: []string{"available"}}},
	}, func(o *ec2.Options) { o.Region = region })
	if err != nil {
		return "", fmt.Errorf("listing availability zones of %s: %w", region, err)
	}
	if len(out.AvailabilityZones) == 0 {
		return "", fmt.Errorf("no available availability zones in %s", region)
	}
	return aws.ToString(out.AvailabilityZones[0].ZoneName), nil
}

package awsaccount

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

// clusterDNS are the DNS entries of a cluster: records in the public zone of
// the base domain and private hosted zones.
type clusterDNS struct {
	publicZoneID string
	records      []r53types.ResourceRecordSet
	privateZones []r53types.HostedZone
}

// ClusterDNS describes the DNS entries the installer or HyperShift create
// for a cluster with the given name.
func (a *Account) ClusterDNS(ctx context.Context, baseDomain, name string) ([]string, error) {
	dns, err := a.findClusterDNS(ctx, baseDomain, name)
	if err != nil {
		return nil, err
	}
	var entries []string
	for _, r := range dns.records {
		entries = append(entries, fmt.Sprintf("record %s %s", r.Type, decodeRecordName(aws.ToString(r.Name))))
	}
	for _, z := range dns.privateZones {
		entries = append(entries, "private hosted zone "+aws.ToString(z.Name))
	}
	return entries, nil
}

// DeleteClusterDNS deletes the DNS entries of a cluster with the given name.
func (a *Account) DeleteClusterDNS(ctx context.Context, baseDomain, name string) error {
	dns, err := a.findClusterDNS(ctx, baseDomain, name)
	if err != nil {
		return err
	}
	if err := a.deleteRecords(ctx, dns.publicZoneID, dns.records); err != nil {
		return err
	}
	for _, z := range dns.privateZones {
		if err := a.deleteHostedZone(ctx, aws.ToString(z.Id)); err != nil {
			return err
		}
	}
	return nil
}

func (a *Account) findClusterDNS(ctx context.Context, baseDomain, name string) (clusterDNS, error) {
	var dns clusterDNS
	zones, err := a.hostedZones(ctx)
	if err != nil {
		return dns, err
	}
	privateZoneNames := []string{
		name + "." + baseDomain + ".",
		name + ".hypershift.local.",
	}
	for _, z := range zones {
		zoneName := aws.ToString(z.Name)
		private := z.Config != nil && z.Config.PrivateZone
		switch {
		case !private && zoneName == baseDomain+".":
			dns.publicZoneID = aws.ToString(z.Id)
		case private && slices.Contains(privateZoneNames, zoneName):
			dns.privateZones = append(dns.privateZones, z)
		}
	}
	if dns.publicZoneID == "" {
		return dns, fmt.Errorf("no public hosted zone for base domain %s", baseDomain)
	}

	records, err := a.records(ctx, dns.publicZoneID)
	if err != nil {
		return dns, err
	}
	recordNames := []string{
		"api." + name + "." + baseDomain + ".",
		"*.apps." + name + "." + baseDomain + ".",
	}
	for _, r := range records {
		if slices.Contains(recordNames, decodeRecordName(aws.ToString(r.Name))) {
			dns.records = append(dns.records, r)
		}
	}
	return dns, nil
}

func (a *Account) hostedZones(ctx context.Context) ([]r53types.HostedZone, error) {
	var zones []r53types.HostedZone
	pages := route53.NewListHostedZonesPaginator(a.route53, &route53.ListHostedZonesInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing hosted zones: %w", err)
		}
		zones = append(zones, page.HostedZones...)
	}
	return zones, nil
}

func (a *Account) records(ctx context.Context, zoneID string) ([]r53types.ResourceRecordSet, error) {
	var records []r53types.ResourceRecordSet
	pages := route53.NewListResourceRecordSetsPaginator(a.route53, &route53.ListResourceRecordSetsInput{HostedZoneId: aws.String(zoneID)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing records of hosted zone %s: %w", zoneID, err)
		}
		records = append(records, page.ResourceRecordSets...)
	}
	return records, nil
}

func (a *Account) deleteRecords(ctx context.Context, zoneID string, records []r53types.ResourceRecordSet) error {
	if len(records) == 0 {
		return nil
	}
	var changes []r53types.Change
	for _, r := range records {
		changes = append(changes, r53types.Change{Action: r53types.ChangeActionDelete, ResourceRecordSet: &r})
	}
	_, err := a.route53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  &r53types.ChangeBatch{Changes: changes},
	})
	if err != nil {
		return fmt.Errorf("deleting records in hosted zone %s: %w", zoneID, err)
	}
	return nil
}

// deleteHostedZone deletes a hosted zone, which requires deleting all its
// records except the NS and SOA records of the zone apex first.
func (a *Account) deleteHostedZone(ctx context.Context, zoneID string) error {
	records, err := a.records(ctx, zoneID)
	if err != nil {
		return err
	}
	records = slices.DeleteFunc(records, func(r r53types.ResourceRecordSet) bool {
		return r.Type == r53types.RRTypeNs || r.Type == r53types.RRTypeSoa
	})
	if err := a.deleteRecords(ctx, zoneID, records); err != nil {
		return err
	}
	_, err = a.route53.DeleteHostedZone(ctx, &route53.DeleteHostedZoneInput{Id: aws.String(zoneID)})
	var notFound *r53types.NoSuchHostedZone
	if err != nil && !errors.As(err, &notFound) {
		return fmt.Errorf("deleting hosted zone %s: %w", zoneID, err)
	}
	return nil
}

// decodeRecordName decodes the octal escape Route53 uses for "*".
func decodeRecordName(name string) string {
	return strings.ReplaceAll(name, `\052`, "*")
}

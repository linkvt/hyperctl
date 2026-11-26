package azureaccount

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v8"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

const (
	// nsRecordTTL is the TTL of the delegation record in seconds.
	nsRecordTTL = 3600
	// aksMaxPods allows the many control plane pods of hosted clusters per
	// node.
	aksMaxPods = 250
	// pollFrequency is how often long-running operations are polled.
	pollFrequency = 15 * time.Second
)

// DNSRecord is a record set of an Azure DNS zone.
type DNSRecord struct {
	// Name is relative to the zone, "@" is the apex.
	Name string
	// Type is the record type, e.g. "A" or "NS".
	Type     string
	Metadata map[string]string
}

// AKSClusterParams configure an AKS management cluster.
type AKSClusterParams struct {
	ResourceGroup string
	Name          string
	Location      string
	VMSize        string
	Nodes         int
	// Zones of the nodes, empty for a location without zones.
	Zones []string
}

// EnsureOIDCIssuer creates a storage account with a publicly readable
// container of the same name unless they exist, and uploads the documents.
func (a *Account) EnsureOIDCIssuer(ctx context.Context, resourceGroup, storageAccount, location string, documents map[string][]byte) error {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return err
	}
	accounts, err := armstorage.NewAccountsClient(subscriptionID, a.cred, nil)
	if err != nil {
		return err
	}
	_, err = accounts.GetProperties(ctx, resourceGroup, storageAccount, nil)
	if isNotFound(err) {
		poller, err := accounts.BeginCreate(ctx, resourceGroup, storageAccount, armstorage.AccountCreateParameters{
			Kind:     new(armstorage.KindStorageV2),
			SKU:      &armstorage.SKU{Name: new(armstorage.SKUNameStandardLRS)},
			Location: new(location),
			Properties: &armstorage.AccountPropertiesCreateParameters{
				AllowBlobPublicAccess: new(true),
			},
		}, nil)
		if err != nil {
			return fmt.Errorf("creating storage account %s: %w", storageAccount, err)
		}
		if _, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency}); err != nil {
			return fmt.Errorf("creating storage account %s: %w", storageAccount, err)
		}
	} else if err != nil {
		return fmt.Errorf("getting storage account %s: %w", storageAccount, err)
	}

	containers, err := armstorage.NewBlobContainersClient(subscriptionID, a.cred, nil)
	if err != nil {
		return err
	}
	container := storageAccount
	_, err = containers.Get(ctx, resourceGroup, storageAccount, container, nil)
	if isNotFound(err) {
		_, err = containers.Create(ctx, resourceGroup, storageAccount, container, armstorage.BlobContainer{
			ContainerProperties: &armstorage.ContainerProperties{PublicAccess: new(armstorage.PublicAccessContainer)},
		}, nil)
	}
	if err != nil {
		return fmt.Errorf("creating blob container %s: %w", container, err)
	}

	// The account key allows uploading without a data plane role.
	keys, err := accounts.ListKeys(ctx, resourceGroup, storageAccount, nil)
	if err != nil {
		return fmt.Errorf("listing keys of storage account %s: %w", storageAccount, err)
	}
	if len(keys.Keys) == 0 {
		return fmt.Errorf("storage account %s has no keys", storageAccount)
	}
	cred, err := azblob.NewSharedKeyCredential(storageAccount, deref(keys.Keys[0].Value))
	if err != nil {
		return err
	}
	blobs, err := azblob.NewClientWithSharedKeyCredential("https://"+storageAccount+".blob.core.windows.net/", cred, nil)
	if err != nil {
		return err
	}
	for path, content := range documents {
		if _, err := blobs.UploadBuffer(ctx, container, path, content, nil); err != nil {
			return fmt.Errorf("uploading %s to storage account %s: %w", path, storageAccount, err)
		}
	}
	return nil
}

// EnsureDNSZone creates a public DNS zone unless it exists and returns its
// name servers.
func (a *Account) EnsureDNSZone(ctx context.Context, resourceGroup, zone string) ([]string, error) {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return nil, err
	}
	client, err := armdns.NewZonesClient(subscriptionID, a.cred, nil)
	if err != nil {
		return nil, err
	}
	var properties *armdns.ZoneProperties
	existing, err := client.Get(ctx, resourceGroup, zone, nil)
	switch {
	case isNotFound(err):
		created, err := client.CreateOrUpdate(ctx, resourceGroup, zone, armdns.Zone{
			Location:   new("global"),
			Properties: &armdns.ZoneProperties{ZoneType: new(armdns.ZoneTypePublic)},
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("creating DNS zone %s: %w", zone, err)
		}
		properties = created.Properties
	case err != nil:
		return nil, fmt.Errorf("getting DNS zone %s: %w", zone, err)
	default:
		properties = existing.Properties
	}
	if properties == nil || len(properties.NameServers) == 0 {
		return nil, fmt.Errorf("DNS zone %s has no name servers", zone)
	}
	var nameServers []string
	for _, ns := range properties.NameServers {
		nameServers = append(nameServers, deref(ns))
	}
	return nameServers, nil
}

// EnsureNSRecord sets the name servers of an NS record set.
func (a *Account) EnsureNSRecord(ctx context.Context, resourceGroup, zone, name string, nameServers []string) error {
	client, err := a.recordSets()
	if err != nil {
		return err
	}
	var records []*armdns.NsRecord
	for _, ns := range nameServers {
		records = append(records, &armdns.NsRecord{Nsdname: new(ns)})
	}
	_, err = client.CreateOrUpdate(ctx, resourceGroup, zone, name, armdns.RecordTypeNS, armdns.RecordSet{
		Properties: &armdns.RecordSetProperties{TTL: new(int64(nsRecordTTL)), NsRecords: records},
	}, nil)
	if err != nil {
		return fmt.Errorf("setting NS record %s in %s: %w", name, zone, err)
	}
	return nil
}

// DNSRecords lists the record sets of a zone, none for a missing zone.
func (a *Account) DNSRecords(ctx context.Context, resourceGroup, zone string) ([]DNSRecord, error) {
	client, err := a.recordSets()
	if err != nil {
		return nil, err
	}
	var records []DNSRecord
	pager := client.NewListAllByDNSZonePager(resourceGroup, zone, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if isNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("listing records of %s: %w", zone, err)
		}
		for _, r := range page.Value {
			record := DNSRecord{
				Name: deref(r.Name),
				// The type is e.g. Microsoft.Network/dnszones/A.
				Type:     deref(r.Type)[strings.LastIndex(deref(r.Type), "/")+1:],
				Metadata: map[string]string{},
			}
			if r.Properties != nil {
				for k, v := range r.Properties.Metadata {
					record.Metadata[k] = deref(v)
				}
			}
			records = append(records, record)
		}
	}
	return records, nil
}

// DeleteDNSRecord deletes a record set.
func (a *Account) DeleteDNSRecord(ctx context.Context, resourceGroup, zone string, record DNSRecord) error {
	client, err := a.recordSets()
	if err != nil {
		return err
	}
	_, err = client.Delete(ctx, resourceGroup, zone, record.Name, armdns.RecordType(record.Type), nil)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("deleting %s record %s in %s: %w", record.Type, record.Name, zone, err)
	}
	return nil
}

func (a *Account) recordSets() (*armdns.RecordSetsClient, error) {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return nil, err
	}
	return armdns.NewRecordSetsClient(subscriptionID, a.cred, nil)
}

// CreateAKSCluster creates an AKS cluster with Azure CNI and network policy
// and waits until it is provisioned.
func (a *Account) CreateAKSCluster(ctx context.Context, p AKSClusterParams) error {
	client, err := a.managedClusters()
	if err != nil {
		return err
	}
	var zones []*string
	for _, zone := range p.Zones {
		zones = append(zones, new(zone))
	}
	poller, err := client.BeginCreateOrUpdate(ctx, p.ResourceGroup, p.Name, armcontainerservice.ManagedCluster{
		Location: new(p.Location),
		Identity: &armcontainerservice.ManagedClusterIdentity{Type: new(armcontainerservice.ResourceIdentityTypeSystemAssigned)},
		Properties: &armcontainerservice.ManagedClusterProperties{
			DNSPrefix: new(p.Name),
			AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{{
				Name:              new("system"),
				Count:             new(int32(p.Nodes)),
				VMSize:            new(p.VMSize),
				AvailabilityZones: zones,
				Mode:              new(armcontainerservice.AgentPoolModeSystem),
				OSType:            new(armcontainerservice.OSTypeLinux),
				OSSKU:             new(armcontainerservice.OSSKUAzureLinux),
				Type:              new(armcontainerservice.AgentPoolTypeVirtualMachineScaleSets),
				MaxPods:           new(int32(aksMaxPods)),
			}},
			NetworkProfile: &armcontainerservice.NetworkProfile{
				NetworkPlugin:   new(armcontainerservice.NetworkPluginAzure),
				NetworkPolicy:   new(armcontainerservice.NetworkPolicyAzure),
				LoadBalancerSKU: new(armcontainerservice.LoadBalancerSKUStandard),
			},
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("creating AKS cluster %s: %w", p.Name, err)
	}
	if _, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency}); err != nil {
		return fmt.Errorf("creating AKS cluster %s: %w", p.Name, err)
	}
	return nil
}

// AKSAdminKubeconfig returns the admin kubeconfig of an AKS cluster.
func (a *Account) AKSAdminKubeconfig(ctx context.Context, resourceGroup, name string) ([]byte, error) {
	client, err := a.managedClusters()
	if err != nil {
		return nil, err
	}
	credentials, err := client.ListClusterAdminCredentials(ctx, resourceGroup, name, nil)
	if err != nil {
		return nil, fmt.Errorf("getting the admin kubeconfig of AKS cluster %s: %w", name, err)
	}
	if len(credentials.Kubeconfigs) == 0 {
		return nil, errors.New("AKS cluster " + name + " returned no admin kubeconfig")
	}
	return credentials.Kubeconfigs[0].Value, nil
}

func (a *Account) managedClusters() (*armcontainerservice.ManagedClustersClient, error) {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return nil, err
	}
	return armcontainerservice.NewManagedClustersClient(subscriptionID, a.cred, nil)
}

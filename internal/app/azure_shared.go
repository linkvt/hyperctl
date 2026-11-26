package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/linkvt/hyperctl/internal/azureaccount"
	"github.com/linkvt/hyperctl/internal/oidc"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// defaultAzureBaseDomainResourceGroup holds the public DNS zone of the
// default Azure base domain.
const defaultAzureBaseDomainResourceGroup = "os4-common"

// storageAccountMaxLen is the maximum length of an Azure storage account name.
const storageAccountMaxLen = 24

// Built-in Azure role definition IDs.
const (
	roleContributor               = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	roleReader                    = "acdd72a7-3385-48ef-bd42-f606fba81ae7"
	roleUserAccessAdministrator   = "18d7d88d-d35e-4fb5-a5c3-7773c20a72d9"
	roleNetworkContributor        = "4d97b98b-1d4f-4787-a291-c67834d212e7"
	roleVirtualMachineContributor = "9980e02c-c2be-4d73-94e8-173b1dc7cf3c"
	roleManagedIdentityOperator   = "f1a07417-d97a-45cb-824c-7a7467783830"
)

// azureShared are the Azure resources that all clusters share and that
// hyperctl keeps when destroying clusters.
type azureShared struct {
	Subscription azureaccount.Subscription
	// Location is where missing shared resources are created.
	Location string
	// ResourceGroup holds the OIDC issuer, the External DNS zone and the
	// workload identities of the hosted clusters.
	ResourceGroup           string
	BaseDomain              string
	BaseDomainResourceGroup string
	OIDCStorageAccount      string
	// DNSZone is the External DNS zone, delegated from the base domain zone.
	DNSZone string
	// ServicePrincipal is the display name of the service principal the
	// hypershift CLI uses, its credentials are in CredentialsFile.
	ServicePrincipal string
	CredentialsFile  string
	// ExternalDNSServicePrincipal is the display name of the service
	// principal External DNS uses, its credentials are in
	// ExternalDNSCredentialsFile.
	ExternalDNSServicePrincipal string
	ExternalDNSCredentialsFile  string
	// SigningKeyFile is the private key of the OIDC issuer.
	SigningKeyFile string
}

// newAzureShared names the shared Azure resources after the config, with
// defaults derived from $USER.
func newAzureShared(dirs xdg.Dirs, cfg config, subscription azureaccount.Subscription, baseDomain, location string) (azureShared, error) {
	user := os.Getenv("USER")
	if user == "" {
		return azureShared{}, errors.New("USER is not set, it names the shared Azure resources")
	}
	dir := azureDir(dirs)
	s := azureShared{
		Subscription:                subscription,
		Location:                    location,
		ResourceGroup:               cmp.Or(cfg.Azure.ResourceGroup, user+"-hyperctl"),
		BaseDomain:                  baseDomain,
		BaseDomainResourceGroup:     cmp.Or(cfg.Azure.BaseDomainResourceGroup, defaultAzureBaseDomainResourceGroup),
		OIDCStorageAccount:          cmp.Or(cfg.Azure.OIDCStorageAccount, defaultStorageAccount(user)),
		DNSZone:                     cmp.Or(cfg.Azure.DNSZone, user+"-hyperctl."+baseDomain),
		ServicePrincipal:            user + "-hyperctl",
		CredentialsFile:             filepath.Join(dir, "credentials.json"),
		ExternalDNSServicePrincipal: user + "-hyperctl-external-dns",
		ExternalDNSCredentialsFile:  filepath.Join(dir, "external-dns-credentials.json"),
		SigningKeyFile:              filepath.Join(dir, "serviceaccount-signer.private"),
	}
	if !strings.HasSuffix(s.DNSZone, "."+baseDomain) {
		return azureShared{}, fmt.Errorf("DNS zone %s is not a subdomain of the base domain %s", s.DNSZone, baseDomain)
	}
	return s, nil
}

// defaultStorageAccount derives a storage account name from the user name,
// which may only contain lowercase letters and digits.
func defaultStorageAccount(user string) string {
	name := strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || (!unicode.IsLower(r) && !unicode.IsDigit(r)) {
			return -1
		}
		return r
	}, strings.ToLower(user)) + "hyperctloidc"
	return name[:min(len(name), storageAccountMaxLen)]
}

// OIDCIssuerURL is the URL of the OIDC issuer in the storage account.
func (s azureShared) OIDCIssuerURL() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net/%s", s.OIDCStorageAccount, s.OIDCStorageAccount)
}

func (s azureShared) subscriptionScope() string {
	return "/subscriptions/" + s.Subscription.ID
}

func (s azureShared) dnsZoneScope() string {
	return dnsZoneScope(s.Subscription.ID, s.ResourceGroup, s.DNSZone)
}

// delegationRecord is the name of the NS record of the DNS zone in the base
// domain zone.
func (s azureShared) delegationRecord() string {
	return strings.TrimSuffix(s.DNSZone, "."+s.BaseDomain)
}

func dnsZoneScope(subscriptionID, resourceGroup, zone string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/dnszones/%s", subscriptionID, resourceGroup, zone)
}

// servicePrincipalCredentials is the credentials file format of the
// hypershift CLI.
type servicePrincipalCredentials struct {
	SubscriptionID string `json:"subscriptionId"`
	TenantID       string `json:"tenantId"`
	ClientID       string `json:"clientId"`
	ClientSecret   string `json:"clientSecret"`
}

// externalDNSCredentials is the credentials file format of External DNS.
type externalDNSCredentials struct {
	TenantID        string `json:"tenantId"`
	SubscriptionID  string `json:"subscriptionId"`
	ResourceGroup   string `json:"resourceGroup"`
	AADClientID     string `json:"aadClientId"`
	AADClientSecret string `json:"aadClientSecret"`
}

// ensureAzureShared creates the shared Azure resources that are missing and
// assigns their roles, so that resources deleted in the meantime are
// recreated.
func ensureAzureShared(ctx context.Context, p progress, account AzureAccount, s azureShared) error {
	var created bool
	err := p.wait(ctx, "Checking resource group "+s.ResourceGroup, func(ctx context.Context) error {
		var err error
		created, err = account.EnsureResourceGroup(ctx, s.ResourceGroup, s.Location)
		return err
	})
	if err != nil {
		return err
	}
	if created {
		p.done("Created resource group %s in %s", s.ResourceGroup, s.Location)
	} else {
		p.done("Resource group %s exists", s.ResourceGroup)
	}

	if err := ensureHypershiftServicePrincipal(ctx, p, account, s); err != nil {
		return err
	}
	if err := ensureOIDCIssuer(ctx, p, account, s); err != nil {
		return err
	}
	if err := ensureExternalDNSZone(ctx, p, account, s); err != nil {
		return err
	}
	return ensureExternalDNSServicePrincipal(ctx, p, account, s)
}

// ensureHypershiftServicePrincipal ensures the service principal of the
// hypershift CLI, which creates identities, infrastructure and clusters in
// the whole subscription.
func ensureHypershiftServicePrincipal(ctx context.Context, p progress, account AzureAccount, s azureShared) error {
	err := p.wait(ctx, "Checking service principal "+s.ServicePrincipal, func(ctx context.Context) error {
		objectID, err := ensureServicePrincipal(ctx, account, s.ServicePrincipal, s.CredentialsFile, "clientId", func(sp azureaccount.ServicePrincipal) any {
			return servicePrincipalCredentials{
				SubscriptionID: s.Subscription.ID,
				TenantID:       s.Subscription.TenantID,
				ClientID:       sp.AppID,
				ClientSecret:   sp.ClientSecret,
			}
		})
		if err != nil {
			return err
		}
		for _, role := range []string{roleContributor, roleUserAccessAdministrator} {
			if err := account.EnsureRoleAssignment(ctx, s.subscriptionScope(), role, objectID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.done("Service principal %s (%s)", s.ServicePrincipal, s.CredentialsFile)
	return nil
}

// ensureExternalDNSServicePrincipal ensures the service principal of External
// DNS, which manages the records of the External DNS zone.
func ensureExternalDNSServicePrincipal(ctx context.Context, p progress, account AzureAccount, s azureShared) error {
	err := p.wait(ctx, "Checking service principal "+s.ExternalDNSServicePrincipal, func(ctx context.Context) error {
		objectID, err := ensureServicePrincipal(ctx, account, s.ExternalDNSServicePrincipal, s.ExternalDNSCredentialsFile, "aadClientId", func(sp azureaccount.ServicePrincipal) any {
			return externalDNSCredentials{
				TenantID:        s.Subscription.TenantID,
				SubscriptionID:  s.Subscription.ID,
				ResourceGroup:   s.ResourceGroup,
				AADClientID:     sp.AppID,
				AADClientSecret: sp.ClientSecret,
			}
		})
		if err != nil {
			return err
		}
		for _, role := range []string{roleReader, roleContributor} {
			if err := account.EnsureRoleAssignment(ctx, s.dnsZoneScope(), role, objectID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.done("Service principal %s (%s)", s.ExternalDNSServicePrincipal, s.ExternalDNSCredentialsFile)
	return nil
}

// ensureServicePrincipal returns the object ID of the service principal in
// the credentials file at path, whose client ID has the key appIDKey. Without
// the file, it creates the service principal and writes the file with the
// content of credentials.
func ensureServicePrincipal(ctx context.Context, account AzureAccount, displayName, path, appIDKey string, credentials func(azureaccount.ServicePrincipal) any) (string, error) {
	appID, found, err := readAppID(path, appIDKey)
	if err != nil {
		return "", err
	}
	if found {
		objectID, exists, err := account.ServicePrincipalObjectID(ctx, appID)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", fmt.Errorf("service principal %s of %s does not exist, delete the file to create a new one", appID, path)
		}
		return objectID, nil
	}

	sp, err := account.CreateServicePrincipal(ctx, displayName)
	if err != nil {
		return "", err
	}
	content, err := json.MarshalIndent(credentials(sp), "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeFile(path, content); err != nil {
		return "", err
	}
	return sp.ObjectID, nil
}

// readAppID reads the application ID from a credentials file and reports
// whether the file exists.
func readAppID(path, key string) (string, bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var credentials map[string]string
	if err := json.Unmarshal(content, &credentials); err != nil {
		return "", false, fmt.Errorf("parsing %s: %w", path, err)
	}
	if credentials[key] == "" {
		return "", false, fmt.Errorf("%s has no %s", path, key)
	}
	return credentials[key], true, nil
}

// ensureOIDCIssuer ensures the signing key and the OIDC issuer publishing its
// public key. The documents are uploaded on every run, which also repairs an
// issuer that lost them.
func ensureOIDCIssuer(ctx context.Context, p progress, account AzureAccount, s azureShared) error {
	privateKey, err := os.ReadFile(s.SigningKeyFile)
	if errors.Is(err, os.ErrNotExist) {
		if privateKey, err = oidc.GenerateKey(); err != nil {
			return err
		}
		if err := writeFile(s.SigningKeyFile, privateKey); err != nil {
			return err
		}
		p.done("Generated the service account signing key %s", s.SigningKeyFile)
	} else if err != nil {
		return err
	}
	publicKey, err := oidc.PublicKey(privateKey)
	if err != nil {
		return fmt.Errorf("%s: %w", s.SigningKeyFile, err)
	}
	documents, err := oidc.Documents(s.OIDCIssuerURL(), publicKey)
	if err != nil {
		return err
	}
	err = p.wait(ctx, "Checking OIDC issuer "+s.OIDCIssuerURL(), func(ctx context.Context) error {
		return account.EnsureOIDCIssuer(ctx, s.ResourceGroup, s.OIDCStorageAccount, s.Location, documents)
	})
	if err != nil {
		return err
	}
	p.done("OIDC issuer %s", s.OIDCIssuerURL())
	return nil
}

// ensureExternalDNSZone ensures the External DNS zone and its delegation from
// the base domain zone.
func ensureExternalDNSZone(ctx context.Context, p progress, account AzureAccount, s azureShared) error {
	err := p.wait(ctx, "Checking DNS zone "+s.DNSZone, func(ctx context.Context) error {
		nameServers, err := account.EnsureDNSZone(ctx, s.ResourceGroup, s.DNSZone)
		if err != nil {
			return err
		}
		return account.EnsureNSRecord(ctx, s.BaseDomainResourceGroup, s.BaseDomain, s.delegationRecord(), nameServers)
	})
	if err != nil {
		return err
	}
	p.done("DNS zone %s, delegated from %s", s.DNSZone, s.BaseDomain)
	return nil
}

// destroyAzureShared deletes the shared Azure resources: the service
// principals, the delegation of the DNS zone and the shared resource group
// with everything in it.
func destroyAzureShared(ctx context.Context, p progress, account AzureAccount, dirs xdg.Dirs, s azureShared) error {
	err := p.wait(ctx, "Deleting service principal "+s.ServicePrincipal, func(ctx context.Context) error {
		return deleteServicePrincipal(ctx, account, s.CredentialsFile, "clientId", s.subscriptionScope())
	})
	if err != nil {
		return err
	}
	p.done("Deleted service principal %s", s.ServicePrincipal)
	err = p.wait(ctx, "Deleting service principal "+s.ExternalDNSServicePrincipal, func(ctx context.Context) error {
		// Its role assignments are deleted with the DNS zone.
		return deleteServicePrincipal(ctx, account, s.ExternalDNSCredentialsFile, "aadClientId", "")
	})
	if err != nil {
		return err
	}
	p.done("Deleted service principal %s", s.ExternalDNSServicePrincipal)
	err = p.wait(ctx, "Deleting the delegation of "+s.DNSZone, func(ctx context.Context) error {
		return account.DeleteDNSRecord(ctx, s.BaseDomainResourceGroup, s.BaseDomain, azureaccount.DNSRecord{Name: s.delegationRecord(), Type: "NS"})
	})
	if err != nil {
		return err
	}
	p.done("Deleted the NS record %s in %s", s.delegationRecord(), s.BaseDomain)
	err = p.wait(ctx, "Deleting resource group "+s.ResourceGroup, func(ctx context.Context) error {
		return account.DeleteResourceGroups(ctx, s.ResourceGroup)
	})
	if err != nil {
		return err
	}
	p.done("Deleted resource group %s with the OIDC issuer, DNS zone and workload identities", s.ResourceGroup)
	if err := os.RemoveAll(azureDir(dirs)); err != nil {
		return err
	}
	p.done("Deleted the credentials and signing key in %s", azureDir(dirs))
	return nil
}

// deleteServicePrincipal deletes the application of the service principal in
// a credentials file, after its role assignments at roleScope, if given.
func deleteServicePrincipal(ctx context.Context, account AzureAccount, path, appIDKey, roleScope string) error {
	appID, found, err := readAppID(path, appIDKey)
	if err != nil || !found {
		return err
	}
	if roleScope != "" {
		objectID, exists, err := account.ServicePrincipalObjectID(ctx, appID)
		if err != nil {
			return err
		}
		if exists {
			if err := account.DeleteRoleAssignments(ctx, roleScope, objectID); err != nil {
				return err
			}
		}
	}
	if err := account.DeleteApplication(ctx, appID); err != nil {
		return err
	}
	return os.Remove(path)
}

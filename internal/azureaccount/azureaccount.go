// Package azureaccount implements the Azure operations hyperctl needs on top
// of the Azure SDK, with the credentials of the Azure CLI login.
package azureaccount

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// Subscription identifies an Azure subscription.
type Subscription struct {
	ID       string
	Name     string
	TenantID string
}

// Account runs operations against the default subscription of the Azure CLI
// login.
type Account struct {
	cred         azcore.TokenCredential
	subscription func() (Subscription, error)
}

// New returns an Account using the Azure CLI login. It does not contact
// Azure, so that it works without a login for AWS clusters.
func New() (*Account, error) {
	cred, err := azidentity.NewAzureCLICredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure CLI credential: %w", err)
	}
	return &Account{cred: cred, subscription: sync.OnceValues(defaultSubscription)}, nil
}

// defaultSubscription returns the subscription the Azure CLI uses.
func defaultSubscription() (Subscription, error) {
	out, err := exec.Command("az", "account", "show", "--output", "json").Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return Subscription{}, fmt.Errorf("az account show: %w: %s, log in with: az login", err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("az account show: %w", err)
	}
	var account struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		TenantID string `json:"tenantId"`
	}
	if err := json.Unmarshal(out, &account); err != nil {
		return Subscription{}, fmt.Errorf("parsing az account show: %w", err)
	}
	return Subscription{ID: account.ID, Name: account.Name, TenantID: account.TenantID}, nil
}

// Subscription returns the default subscription of the Azure CLI login.
func (a *Account) Subscription(context.Context) (Subscription, error) {
	return a.subscription()
}

func (a *Account) subscriptionID() (string, error) {
	s, err := a.subscription()
	return s.ID, err
}

// AvailableZones returns the zones of a location that offer a VM size
// without restrictions.
func (a *Account) AvailableZones(ctx context.Context, location, vmSize string) ([]string, error) {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return nil, err
	}
	client, err := armcompute.NewResourceSKUsClient(subscriptionID, a.cred, nil)
	if err != nil {
		return nil, err
	}
	pager := client.NewListPager(&armcompute.ResourceSKUsClientListOptions{Filter: new(fmt.Sprintf("location eq '%s'", location))})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing VM sizes in %s: %w", location, err)
		}
		for _, sku := range page.Value {
			if deref(sku.ResourceType) != "virtualMachines" || !strings.EqualFold(deref(sku.Name), vmSize) {
				continue
			}
			return unrestrictedZones(sku, location, vmSize)
		}
	}
	return nil, fmt.Errorf("VM size %s is not offered in location %s", vmSize, location)
}

// unrestrictedZones returns the zones of the location in which the subscription
// may use the SKU.
func unrestrictedZones(sku *armcompute.ResourceSKU, location, vmSize string) ([]string, error) {
	restricted := map[string]bool{}
	for _, r := range sku.Restrictions {
		if r.Type == nil || r.RestrictionInfo == nil {
			continue
		}
		switch *r.Type {
		case armcompute.ResourceSKURestrictionsTypeLocation:
			return nil, fmt.Errorf("VM size %s is restricted in location %s for the subscription (%s), choose another location", vmSize, location, deref((*string)(r.ReasonCode)))
		case armcompute.ResourceSKURestrictionsTypeZone:
			for _, zone := range r.RestrictionInfo.Zones {
				restricted[deref(zone)] = true
			}
		}
	}
	var zones []string
	for _, info := range sku.LocationInfo {
		if !strings.EqualFold(deref(info.Location), location) {
			continue
		}
		for _, zone := range info.Zones {
			if !restricted[deref(zone)] {
				zones = append(zones, deref(zone))
			}
		}
	}
	slices.Sort(zones)
	if len(zones) == 0 && len(restricted) > 0 {
		return nil, fmt.Errorf("VM size %s is restricted in all zones of location %s for the subscription, choose another location", vmSize, location)
	}
	return zones, nil
}

// EnsureResourceGroup creates a resource group unless it exists and reports
// whether it was created.
func (a *Account) EnsureResourceGroup(ctx context.Context, name, location string) (bool, error) {
	client, err := a.resourceGroups()
	if err != nil {
		return false, err
	}
	exists, err := client.CheckExistence(ctx, name, nil)
	if err != nil {
		return false, fmt.Errorf("checking resource group %s: %w", name, err)
	}
	if exists.Success {
		return false, nil
	}
	if _, err := client.CreateOrUpdate(ctx, name, armresources.ResourceGroup{Location: new(location)}, nil); err != nil {
		return false, fmt.Errorf("creating resource group %s: %w", name, err)
	}
	return true, nil
}

// DeleteResourceGroups deletes resource groups in parallel and waits until
// they are gone.
func (a *Account) DeleteResourceGroups(ctx context.Context, names ...string) error {
	client, err := a.resourceGroups()
	if err != nil {
		return err
	}
	pollers := map[string]*runtime.Poller[armresources.ResourceGroupsClientDeleteResponse]{}
	for _, name := range names {
		poller, err := client.BeginDelete(ctx, name, nil)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("deleting resource group %s: %w", name, err)
		}
		pollers[name] = poller
	}
	for name, poller := range pollers {
		if _, err := poller.PollUntilDone(ctx, nil); err != nil {
			return fmt.Errorf("deleting resource group %s: %w", name, err)
		}
	}
	return nil
}

func (a *Account) resourceGroups() (*armresources.ResourceGroupsClient, error) {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return nil, err
	}
	return armresources.NewResourceGroupsClient(subscriptionID, a.cred, nil)
}

// isNotFound reports whether err is an Azure response with status 404.
func isNotFound(err error) bool {
	respErr, ok := errors.AsType[*azcore.ResponseError](err)
	return ok && respErr.StatusCode == http.StatusNotFound
}

// errorCode returns the error code of an Azure response error.
func errorCode(err error) string {
	if respErr, ok := errors.AsType[*azcore.ResponseError](err); ok {
		return respErr.ErrorCode
	}
	return ""
}

// deref returns the value of an optional string from the SDK, empty for nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

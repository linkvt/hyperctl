package azureaccount

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/google/uuid"
)

const (
	// principalNotFoundRetries and principalNotFoundInterval bound waiting
	// for a new principal to replicate to the role assignment service.
	principalNotFoundRetries  = 12
	principalNotFoundInterval = 10 * time.Second
)

// EnsureRoleAssignment assigns a built-in role to a service principal at a
// scope unless it is assigned. The assignment name is derived from its
// content, so that assigning again is a no-op.
func (a *Account) EnsureRoleAssignment(ctx context.Context, scope, roleDefinitionID, principalID string) error {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return err
	}
	client, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, a.cred, nil)
	if err != nil {
		return err
	}
	name := uuid.NewSHA1(uuid.NameSpaceURL, []byte(scope+"|"+roleDefinitionID+"|"+principalID)).String()
	params := armauthorization.RoleAssignmentCreateParameters{Properties: &armauthorization.RoleAssignmentProperties{
		PrincipalID:      new(principalID),
		PrincipalType:    new(armauthorization.PrincipalTypeServicePrincipal),
		RoleDefinitionID: new("/subscriptions/" + subscriptionID + "/providers/Microsoft.Authorization/roleDefinitions/" + roleDefinitionID),
	}}
	for range principalNotFoundRetries {
		_, err = client.Create(ctx, scope, name, params, nil)
		switch errorCode(err) {
		case "":
			return err
		case "RoleAssignmentExists":
			// Assigned before, e.g. with the Azure CLI under another name.
			return nil
		case "PrincipalNotFound":
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(principalNotFoundInterval):
			}
		default:
			return fmt.Errorf("assigning role %s to %s at %s: %w", roleDefinitionID, principalID, scope, err)
		}
	}
	return fmt.Errorf("assigning role %s to %s at %s: %w", roleDefinitionID, principalID, scope, err)
}

// DeleteRoleAssignments deletes the role assignments of a principal at
// exactly the scope, not those inherited from or below it.
func (a *Account) DeleteRoleAssignments(ctx context.Context, scope, principalID string) error {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return err
	}
	client, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, a.cred, nil)
	if err != nil {
		return err
	}
	// The API rejects a principalId filter on some scopes, atScope() lists
	// the assignments at the scope and above it.
	pager := client.NewListForScopePager(scope, &armauthorization.RoleAssignmentsClientListForScopeOptions{
		Filter: new("atScope()"),
	})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if isNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("listing role assignments at %s: %w", scope, err)
		}
		for _, assignment := range page.Value {
			if assignment.Properties == nil ||
				!strings.EqualFold(deref(assignment.Properties.Scope), scope) ||
				!strings.EqualFold(deref(assignment.Properties.PrincipalID), principalID) {
				continue
			}
			if _, err := client.DeleteByID(ctx, deref(assignment.ID), nil); err != nil && !isNotFound(err) {
				return fmt.Errorf("deleting role assignment %s: %w", deref(assignment.ID), err)
			}
		}
	}
	return nil
}

// IdentityPrincipalID returns the principal ID of a user-assigned managed
// identity and whether it exists.
func (a *Account) IdentityPrincipalID(ctx context.Context, resourceGroup, name string) (string, bool, error) {
	client, err := a.identities()
	if err != nil {
		return "", false, err
	}
	identity, err := client.Get(ctx, resourceGroup, name, nil)
	if isNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("getting identity %s: %w", name, err)
	}
	if identity.Properties == nil {
		return "", false, fmt.Errorf("identity %s has no principal ID", name)
	}
	return deref(identity.Properties.PrincipalID), true, nil
}

func (a *Account) identities() (*armmsi.UserAssignedIdentitiesClient, error) {
	subscriptionID, err := a.subscriptionID()
	if err != nil {
		return nil, err
	}
	return armmsi.NewUserAssignedIdentitiesClient(subscriptionID, a.cred, nil)
}

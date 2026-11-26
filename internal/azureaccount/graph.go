package azureaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

const graphURL = "https://graph.microsoft.com/v1.0"

// errGraphNotFound is returned by graph for responses with status 404.
var errGraphNotFound = errors.New("not found")

// ServicePrincipal is a service principal with a new client secret.
type ServicePrincipal struct {
	AppID        string
	ObjectID     string
	ClientSecret string
}

type graphApplication struct {
	ID    string `json:"id"`
	AppID string `json:"appId"`
}

type graphServicePrincipal struct {
	ID string `json:"id"`
}

// CreateServicePrincipal returns the service principal of the application
// with the display name, creating both if missing, and adds a new client
// secret to the application.
func (a *Account) CreateServicePrincipal(ctx context.Context, displayName string) (ServicePrincipal, error) {
	var apps struct {
		Value []graphApplication `json:"value"`
	}
	if err := a.graph(ctx, http.MethodGet, "/applications?"+filter("displayName eq '"+displayName+"'"), nil, &apps); err != nil {
		return ServicePrincipal{}, fmt.Errorf("looking up application %s: %w", displayName, err)
	}
	var application graphApplication
	switch len(apps.Value) {
	case 0:
		if err := a.graph(ctx, http.MethodPost, "/applications", map[string]string{"displayName": displayName}, &application); err != nil {
			return ServicePrincipal{}, fmt.Errorf("creating application %s: %w", displayName, err)
		}
	case 1:
		application = apps.Value[0]
	default:
		return ServicePrincipal{}, fmt.Errorf("found %d applications named %s, delete all but one", len(apps.Value), displayName)
	}

	objectID, found, err := a.ServicePrincipalObjectID(ctx, application.AppID)
	if err != nil {
		return ServicePrincipal{}, err
	}
	if !found {
		var sp graphServicePrincipal
		if err := a.graph(ctx, http.MethodPost, "/servicePrincipals", map[string]string{"appId": application.AppID}, &sp); err != nil {
			return ServicePrincipal{}, fmt.Errorf("creating service principal of %s: %w", displayName, err)
		}
		objectID = sp.ID
	}

	var password struct {
		SecretText string `json:"secretText"`
	}
	body := map[string]any{"passwordCredential": map[string]string{"displayName": "hyperctl"}}
	if err := a.graph(ctx, http.MethodPost, "/applications/"+application.ID+"/addPassword", body, &password); err != nil {
		return ServicePrincipal{}, fmt.Errorf("adding a client secret to %s: %w", displayName, err)
	}
	return ServicePrincipal{AppID: application.AppID, ObjectID: objectID, ClientSecret: password.SecretText}, nil
}

// ServicePrincipalObjectID returns the object ID of the service principal of
// an application and whether it exists.
func (a *Account) ServicePrincipalObjectID(ctx context.Context, appID string) (string, bool, error) {
	var sps struct {
		Value []graphServicePrincipal `json:"value"`
	}
	if err := a.graph(ctx, http.MethodGet, "/servicePrincipals?"+filter("appId eq '"+appID+"'"), nil, &sps); err != nil {
		return "", false, fmt.Errorf("looking up service principal of %s: %w", appID, err)
	}
	if len(sps.Value) == 0 {
		return "", false, nil
	}
	return sps.Value[0].ID, true, nil
}

// DeleteApplication deletes an application, which deletes its service
// principal.
func (a *Account) DeleteApplication(ctx context.Context, appID string) error {
	err := a.graph(ctx, http.MethodDelete, "/applications(appId='"+url.PathEscape(appID)+"')", nil, nil)
	if errors.Is(err, errGraphNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting application %s: %w", appID, err)
	}
	return nil
}

func filter(expression string) string {
	return url.Values{"$filter": {expression}}.Encode()
}

// graph sends a request to Microsoft Graph and decodes the response into out,
// if given.
func (a *Account) graph(ctx context.Context, method, path string, body, out any) error {
	token, err := a.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{"https://graph.microsoft.com/.default"}})
	if err != nil {
		return fmt.Errorf("getting a Microsoft Graph token: %w", err)
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, graphURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return errGraphNotFound
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, respBody)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

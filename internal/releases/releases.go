// Package releases reads OCP releases from the release controller API.
package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
)

// DefaultURL is the release controller for amd64 releases.
const DefaultURL = "https://amd64.ocp.releases.ci.openshift.org"

// PhaseAccepted is the phase of releases that passed their blocking jobs.
const PhaseAccepted = "Accepted"

// Tag is a release in a release stream.
type Tag struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	PullSpec string `json:"pullSpec"`
}

// Client reads from the release controller at BaseURL.
type Client struct {
	BaseURL string
}

// Streams returns the names of all release streams, sorted.
func (c Client) Streams(ctx context.Context) ([]string, error) {
	var streams map[string][]string
	if err := c.get(ctx, "/api/v1/releasestreams/all", &streams); err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(streams)), nil
}

// Tags returns the releases of a stream, newest first.
func (c Client) Tags(ctx context.Context, stream string) ([]Tag, error) {
	var out struct {
		Tags []Tag `json:"tags"`
	}
	if err := c.get(ctx, "/api/v1/releasestream/"+url.PathEscape(stream)+"/tags", &out); err != nil {
		return nil, err
	}
	return out.Tags, nil
}

func (c Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("querying release controller: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("querying release controller %s: %s", req.URL, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decoding release controller response from %s: %w", req.URL, err)
	}
	return nil
}

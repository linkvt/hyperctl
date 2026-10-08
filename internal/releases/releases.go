// Package releases reads OCP releases from the release controller API.
package releases

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// DefaultURL is the release controller for amd64 releases.
const DefaultURL = "https://amd64.ocp.releases.ci.openshift.org"

// PhaseAccepted is the phase of releases that passed blocking jobs or were manually accepted.
const PhaseAccepted = "Accepted"

// Tag is a release in a release stream.
type Tag struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	PullSpec string `json:"pullSpec"`
}

// TagDetails contains a release's phase and verification results.
type TagDetails struct {
	Name    string      `json:"name"`
	Phase   string      `json:"phase"`
	Results *TagResults `json:"results"`
}

// TagResults contains verification results grouped by job type.
type TagResults struct {
	BlockingJobs  map[string]JobResult `json:"blockingJobs"`
	InformingJobs map[string]JobResult `json:"informingJobs"`
	AsyncJobs     map[string]JobResult `json:"asyncJobs"`
}

// JobResult is the final result of one verification job.
type JobResult struct {
	State string `json:"state"`
}

// Client reads from the release controller at BaseURL.
type Client struct {
	BaseURL string
}

// Streams returns the names of the release streams with accepted releases,
// or of all streams if all is set, newest first.
func (c Client) Streams(ctx context.Context, all bool) ([]string, error) {
	path := "/api/v1/releasestreams/accepted"
	if all {
		path = "/api/v1/releasestreams/all"
	}
	var streams map[string][]string
	if err := c.get(ctx, path, &streams); err != nil {
		return nil, err
	}
	maps.DeleteFunc(streams, func(_ string, tags []string) bool { return len(tags) == 0 })
	return slices.SortedFunc(maps.Keys(streams), func(a, b string) int {
		return naturalCompare(b, a)
	}), nil
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

// TagDetails returns the phase and verification results for a release.
func (c Client) TagDetails(ctx context.Context, stream, tag string) (TagDetails, error) {
	var details TagDetails
	path := "/api/v1/releasestream/" + url.PathEscape(stream) + "/release/" + url.PathEscape(tag) + "?format=short"
	if err := c.get(ctx, path, &details); err != nil {
		return TagDetails{}, err
	}
	return details, nil
}

func (r TagResults) FailedJobCount() int {
	return countFailedJobs(r.BlockingJobs) + countFailedJobs(r.InformingJobs) + countFailedJobs(r.AsyncJobs)
}

func countFailedJobs(jobs map[string]JobResult) int {
	count := 0
	for _, job := range jobs {
		if job.State == "Failed" {
			count++
		}
	}
	return count
}

func (r TagResults) HasFailures() bool {
	return r.FailedJobCount() > 0
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

// naturalCompare compares strings with runs of digits compared by their
// numeric value, so that 5.10 sorts after 5.9.
func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		aChunk, aRest := leadingChunk(a)
		bChunk, bRest := leadingChunk(b)
		if c := compareChunks(aChunk, bChunk); c != 0 {
			return c
		}
		a, b = aRest, bRest
	}
	return cmp.Compare(len(a), len(b))
}

// leadingChunk splits s after its leading run of digits or non-digits.
func leadingChunk(s string) (string, string) {
	digits := isDigit(s[0])
	i := 1
	for i < len(s) && isDigit(s[i]) == digits {
		i++
	}
	return s[:i], s[i:]
}

func compareChunks(a, b string) int {
	if isDigit(a[0]) && isDigit(b[0]) {
		a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		if c := cmp.Compare(len(a), len(b)); c != 0 {
			return c
		}
	}
	return strings.Compare(a, b)
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

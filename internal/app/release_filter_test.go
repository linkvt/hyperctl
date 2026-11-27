package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/linkvt/hyperctl/internal/releases"
)

func TestNoReleasesWarning(t *testing.T) {
	const stream = "5.1.0-0.nightly"
	tests := map[string]struct {
		filter releaseFilter
		want   string
	}{
		"accepted without failures": {
			filter: releaseFilterAcceptedNoFailures,
			want:   `no Accepted releases without failed jobs in stream ` + stream + `; try "Accepted (including failed jobs)" or "All phases (also Ready and Rejected)"`,
		},
		"accepted": {
			filter: releaseFilterAccepted,
			want:   `no Accepted releases in stream ` + stream + `; try "All phases (also Ready and Rejected)"`,
		},
		"all phases": {
			filter: releaseFilterAll,
			want:   `no releases in stream ` + stream,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := noReleasesWarning(stream, tc.filter); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReleaseOptionLabel(t *testing.T) {
	tag := releases.Tag{Name: "release", Phase: releases.PhaseAccepted}
	tests := map[string]struct {
		failedJobs int
		want       string
	}{
		"no failed jobs": {want: "release (Accepted)"},
		"failed jobs":    {failedJobs: 3, want: "release (Accepted · 3 failed)"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := releaseOptionLabel(tag, tc.failedJobs); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFilterBrowseTagsFailsWithoutJobResults(t *testing.T) {
	const stream = "5.1.0-0.nightly"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"accepted","phase":"Accepted"}`))
	}))
	defer server.Close()

	_, _, err := filterBrowseTags(context.Background(), releases.Client{BaseURL: server.URL}, stream, []releases.Tag{
		{Name: "accepted", Phase: releases.PhaseAccepted},
	}, releaseFilterAcceptedNoFailures)
	if err == nil {
		t.Fatal("expected missing job results to fail the safe filter")
	}
}

func TestFilterBrowseTags(t *testing.T) {
	const stream = "5.1.0-0.nightly"
	const cleanTag = "clean"
	const failedTag = "failed"
	const rejectedTag = "rejected"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/releasestream/" + stream + "/release/" + cleanTag:
			_, _ = w.Write([]byte(`{"name":"clean","phase":"Accepted","results":{"blockingJobs":{"e2e":{"state":"Succeeded"}}}}`))
		case "/api/v1/releasestream/" + stream + "/release/" + failedTag:
			_, _ = w.Write([]byte(`{"name":"failed","phase":"Accepted","results":{"informingJobs":{"console":{"state":"Failed"}}}}`))
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tags := []releases.Tag{
		{Name: cleanTag, Phase: releases.PhaseAccepted},
		{Name: failedTag, Phase: releases.PhaseAccepted},
		{Name: rejectedTag, Phase: "Rejected"},
	}
	client := releases.Client{BaseURL: server.URL}
	tests := map[string]struct {
		filter       releaseFilter
		want         []string
		wantFailures map[string]int
	}{
		"accepted with no failed jobs": {
			filter: releaseFilterAcceptedNoFailures,
			want:   []string{cleanTag},
		},
		"accepted including failures": {
			filter:       releaseFilterAccepted,
			want:         []string{cleanTag, failedTag},
			wantFailures: map[string]int{failedTag: 1},
		},
		"all phases": {
			filter: releaseFilterAll,
			want:   []string{cleanTag, failedTag, rejectedTag},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, failures, err := filterBrowseTags(context.Background(), client, stream, tags, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, tag := range got {
				names = append(names, tag.Name)
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Errorf("got %v, want %v", names, tc.want)
			}
			if !reflect.DeepEqual(failures, tc.wantFailures) {
				t.Errorf("failed job counts = %v, want %v", failures, tc.wantFailures)
			}
		})
	}
}

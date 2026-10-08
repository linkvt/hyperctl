package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientTagDetails(t *testing.T) {
	const stream = "5.1.0-0.nightly"
	const tag = "5.1.0-0.nightly-2026-09-30-105558"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/releasestream/"+stream+"/release/"+tag {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("format"); got != "short" {
			t.Errorf("format = %q, want short", got)
		}
		_, _ = w.Write([]byte(`{"name":"` + tag + `","phase":"Accepted","results":{
			"blockingJobs":{"e2e":{"state":"Failed"}},
			"informingJobs":{"console":{"state":"Succeeded"}},
			"asyncJobs":{"agent":{"state":"Succeeded"}}
		}}`))
	}))
	defer server.Close()

	details, err := (Client{BaseURL: server.URL}).TagDetails(context.Background(), stream, tag)
	if err != nil {
		t.Fatal(err)
	}
	if details.Name != tag {
		t.Errorf("got tag name %q, want %q", details.Name, tag)
	}
	if details.Results == nil {
		t.Fatal("expected results")
	}
	if !details.Results.HasFailures() {
		t.Error("expected blocking job failure to be reported")
	}
}

func TestTagResultsHasFailures(t *testing.T) {
	tests := map[string]struct {
		results TagResults
		want    bool
	}{
		"no jobs": {},
		"all jobs succeeded": {
			results: TagResults{
				BlockingJobs:  map[string]JobResult{"blocking": {State: "Succeeded"}},
				InformingJobs: map[string]JobResult{"informing": {State: "Succeeded"}},
				AsyncJobs:     map[string]JobResult{"async": {State: "Succeeded"}},
			},
		},
		"blocking failure": {
			results: TagResults{BlockingJobs: map[string]JobResult{"blocking": {State: "Failed"}}},
			want:    true,
		},
		"informing failure": {
			results: TagResults{InformingJobs: map[string]JobResult{"informing": {State: "Failed"}}},
			want:    true,
		},
		"async failure": {
			results: TagResults{AsyncJobs: map[string]JobResult{"async": {State: "Failed"}}},
			want:    true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tc.results.HasFailures(); got != tc.want {
				t.Errorf("HasFailures() = %t, want %t", got, tc.want)
			}
		})
	}
}

package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleasesListsStreams(t *testing.T) {
	env := newTestEnv(t)
	env.releasesURL = fakeReleaseController(t)

	out := env.run(t, "releases")

	if want := "4-stable\n5-dev-preview\n5.1.0-0.nightly\n"; out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestReleasesListsTagsOfStream(t *testing.T) {
	tests := map[string]struct {
		args []string
		want []string
		skip []string
	}{
		"accepted only": {
			args: []string{"releases", "5.1.0-0.nightly"},
			want: []string{"5.1.0-0.nightly-2026-09-24-000000", "Accepted", "registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-24-000000"},
			skip: []string{"Rejected", "Ready"},
		},
		"all phases": {
			args: []string{"releases", "5.1.0-0.nightly", "--all"},
			want: []string{"5.1.0-0.nightly-2026-09-25-000000", "Ready", "5.1.0-0.nightly-2026-09-23-000000", "Rejected", "Accepted"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.releasesURL = fakeReleaseController(t)

			out := env.run(t, tc.args...)

			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output:\n%s", want, out)
				}
			}
			for _, skip := range tc.skip {
				if strings.Contains(out, skip) {
					t.Errorf("expected no %q in output:\n%s", skip, out)
				}
			}
		})
	}
}

// fakeReleaseController serves a minimal release controller API.
func fakeReleaseController(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/releasestreams/all", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"5.1.0-0.nightly":["5.1.0-0.nightly-2026-09-25-000000"],"4-stable":["4.22.16"],"5-dev-preview":["5.1.0-ec.0"]}`))
	})
	mux.HandleFunc("GET /api/v1/releasestream/5.1.0-0.nightly/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"5.1.0-0.nightly","tags":[
			{"name":"5.1.0-0.nightly-2026-09-25-000000","phase":"Ready","pullSpec":"registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-25-000000"},
			{"name":"5.1.0-0.nightly-2026-09-24-000000","phase":"Accepted","pullSpec":"registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-24-000000"},
			{"name":"5.1.0-0.nightly-2026-09-23-000000","phase":"Rejected","pullSpec":"registry.ci.openshift.org/ocp/release-5:5.1.0-0.nightly-2026-09-23-000000"}
		]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

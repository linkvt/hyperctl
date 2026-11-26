package oc

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/linkvt/hyperctl/internal/process/processtest"
)

const testKubeconfig = "/mgmt/kubeconfig"

func TestCLICommands(t *testing.T) {
	tests := map[string]struct {
		results []processtest.Result
		run     func(context.Context, CLI) error
		want    [][]string
	}{
		"ready": {
			run: func(ctx context.Context, c CLI) error {
				c.Ready(ctx, testKubeconfig)
				return nil
			},
			want: [][]string{{"get", "--raw", "/readyz", "--request-timeout=10s"}},
		},
		"hosted cluster available": {
			run: func(ctx context.Context, c CLI) error {
				_, err := c.HostedClusterAvailable(ctx, testKubeconfig, "workload")
				return err
			},
			want: [][]string{{"get", "hostedcluster", "workload", "-n", "clusters",
				"-o", `jsonpath={.status.conditions[?(@.type=="Available")].status}`,
				"--request-timeout=10s"}},
		},
		"annotations": {
			results: []processtest.Result{{Stdout: "{}"}},
			run: func(ctx context.Context, c CLI) error {
				_, err := c.Annotations(ctx, testKubeconfig, "hostedcluster", "clusters", "workload")
				return err
			},
			want: [][]string{{"get", "hostedcluster", "workload", "-n", "clusters", "--request-timeout=10s", "-o", "json"}},
		},
		"annotate": {
			run: func(ctx context.Context, c CLI) error {
				return c.Annotate(ctx, testKubeconfig, "hostedcontrolplane", "clusters-workload", "workload", "example.com/image", "quay.io/test:dev")
			},
			want: [][]string{{"annotate", "hostedcontrolplane", "workload", "-n", "clusters-workload", "--overwrite", "example.com/image=quay.io/test:dev"}},
		},
		"remove annotation": {
			run: func(ctx context.Context, c CLI) error {
				return c.RemoveAnnotation(ctx, testKubeconfig, "hostedcluster", "clusters", "workload", "example.com/image")
			},
			want: [][]string{{"annotate", "hostedcluster", "workload", "-n", "clusters", "example.com/image-"}},
		},
		"delete pods": {
			run: func(ctx context.Context, c CLI) error {
				return c.DeletePods(ctx, testKubeconfig, "clusters-workload", "app=karpenter")
			},
			want: [][]string{{"delete", "pod", "-n", "clusters-workload", "-l", "app=karpenter"}},
		},
		"apply server side": {
			run: func(ctx context.Context, c CLI) error {
				return c.ApplyServerSide(ctx, testKubeconfig, "https://example.com/crd.yaml")
			},
			want: [][]string{{"apply", "--server-side", "-f", "https://example.com/crd.yaml"}},
		},
		"hosted kubeconfig": {
			results: []processtest.Result{
				{Stdout: `{"status":{"kubeconfig":{"name":"workload-admin-kubeconfig"}}}`},
				{Stdout: testSecret("kind: Config")},
			},
			run: func(ctx context.Context, c CLI) error {
				_, err := c.HostedKubeconfig(ctx, testKubeconfig, "workload")
				return err
			},
			want: [][]string{
				{"get", "hostedcluster", "workload", "-n", "clusters", "--ignore-not-found", "--request-timeout=10s", "-o", "json"},
				{"get", "secret", "workload-admin-kubeconfig", "-n", "clusters", "--ignore-not-found", "--request-timeout=10s", "-o", "json"},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := &processtest.Recorder{Results: tc.results}
			cli := CLI{runner: recorder}

			if err := tc.run(context.Background(), cli); err != nil {
				t.Fatal(err)
			}

			var want []processtest.Call
			for _, args := range tc.want {
				want = append(want, processtest.Call{Path: "oc", Args: append([]string{"--kubeconfig", testKubeconfig}, args...)})
			}
			if !reflect.DeepEqual(recorder.Calls, want) {
				t.Errorf("calls:\ngot:  %q\nwant: %q", recorder.Calls, want)
			}
		})
	}
}

func TestCLIReady(t *testing.T) {
	tests := map[string]struct {
		result processtest.Result
		want   bool
	}{
		"responding API":     {result: processtest.Result{Stdout: "ok"}, want: true},
		"not responding API": {result: processtest.Result{Err: errors.New("exit status 1")}, want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cli := CLI{runner: &processtest.Recorder{Results: []processtest.Result{tc.result}}}

			if got := cli.Ready(context.Background(), testKubeconfig); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCLIHostedClusterAvailable(t *testing.T) {
	tests := map[string]struct {
		result    processtest.Result
		want      string
		wantError string
	}{
		"available": {
			result: processtest.Result{Stdout: "True\n"},
			want:   "True",
		},
		"failure reports stderr": {
			result:    processtest.Result{Stderr: "Forbidden\n", Err: errors.New("exit status 1")},
			wantError: "getting hosted cluster workload: exit status 1: Forbidden",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cli := CLI{runner: &processtest.Recorder{Results: []processtest.Result{tc.result}}}

			got, err := cli.HostedClusterAvailable(context.Background(), testKubeconfig, "workload")

			checkError(t, err, tc.wantError)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCLIAnnotations(t *testing.T) {
	cli := CLI{runner: &processtest.Recorder{Results: []processtest.Result{
		{Stdout: `{"metadata":{"annotations":{"example.com/image":"quay.io/test:dev"}}}`},
	}}}

	got, err := cli.Annotations(context.Background(), testKubeconfig, "hostedcluster", "clusters", "workload")
	if err != nil {
		t.Fatal(err)
	}

	if want := map[string]string{"example.com/image": "quay.io/test:dev"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCLIHostedKubeconfig(t *testing.T) {
	hostedCluster := processtest.Result{Stdout: `{"status":{"kubeconfig":{"name":"workload-admin-kubeconfig"}}}`}
	tests := map[string]struct {
		results   []processtest.Result
		want      string
		wantError string
	}{
		"kubeconfig": {
			results: []processtest.Result{hostedCluster, {Stdout: testSecret("kind: Config")}},
			want:    "kind: Config",
		},
		"no hosted cluster": {
			results: []processtest.Result{{}},
		},
		"no secret reference": {
			results: []processtest.Result{{Stdout: `{"status":{}}`}},
		},
		"no secret": {
			results: []processtest.Result{hostedCluster, {}},
		},
		"secret without kubeconfig": {
			results:   []processtest.Result{hostedCluster, {Stdout: `{"data":{}}`}},
			wantError: "hosted kubeconfig secret workload-admin-kubeconfig has no kubeconfig",
		},
		"failure reports stderr": {
			results:   []processtest.Result{{Stderr: "connection refused\n", Err: errors.New("exit status 1")}},
			wantError: "getting hosted cluster workload: exit status 1: connection refused",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cli := CLI{runner: &processtest.Recorder{Results: tc.results}}

			got, err := cli.HostedKubeconfig(context.Background(), testKubeconfig, "workload")

			checkError(t, err, tc.wantError)
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// testSecret returns the JSON of a secret holding a kubeconfig.
func testSecret(kubeconfig string) string {
	return `{"data":{"kubeconfig":"` + base64.StdEncoding.EncodeToString([]byte(kubeconfig)) + `"}}`
}

func checkError(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Errorf("unexpected error: %v", err)
	case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
		t.Errorf("got error %v, want %q", err, want)
	}
}

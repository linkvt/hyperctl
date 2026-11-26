package ocp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/linkvt/hyperctl/internal/process/processtest"
)

var testRelease = Release{Name: "5.1.0-ec.0", PullSpec: "quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64"}

func TestCLICheckPullable(t *testing.T) {
	recorder := &processtest.Recorder{}
	cli := CLI{runner: recorder}

	if err := cli.CheckPullable(context.Background(), testRelease, "/pull-secret.json"); err != nil {
		t.Fatal(err)
	}

	want := []processtest.Call{{Path: "oc", Args: []string{"adm", "release", "info",
		"--registry-config=/pull-secret.json",
		"quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
	}}}
	if !reflect.DeepEqual(recorder.Calls, want) {
		t.Errorf("calls:\ngot:  %q\nwant: %q", recorder.Calls, want)
	}
}

func TestCLICheckPullableReportsOutput(t *testing.T) {
	cli := CLI{runner: &processtest.Recorder{Results: []processtest.Result{
		{Stderr: "error: unauthorized: authentication required\n", Err: errors.New("exit status 1")},
	}}}

	err := cli.CheckPullable(context.Background(), testRelease, "/pull-secret.json")

	want := "the release image quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64 cannot be pulled with the pull secret /pull-secret.json:\nerror: unauthorized: authentication required"
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

func TestCLICachedInstaller(t *testing.T) {
	cacheDir := t.TempDir()
	cli := CLI{}

	if _, found := cli.CachedInstaller(cacheDir, testRelease); found {
		t.Fatal("expected no cached installer in an empty cache")
	}
	path := filepath.Join(cacheDir, "5.1.0-ec.0", "openshift-install")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o755); err != nil {
		t.Fatal(err)
	}

	installer, found := cli.CachedInstaller(cacheDir, testRelease)

	if !found || installer.Path != path {
		t.Errorf("got %v, %v, want %s", installer, found, path)
	}
}

func TestCLIExtractInstaller(t *testing.T) {
	cacheDir := t.TempDir()
	recorder := &processtest.Recorder{}
	cli := CLI{runner: recorder}

	installer, err := cli.ExtractInstaller(context.Background(), cacheDir, testRelease, "/pull-secret.json")
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(cacheDir, "5.1.0-ec.0", "openshift-install"); installer.Path != want {
		t.Errorf("installer: got %s, want %s", installer.Path, want)
	}
	if len(recorder.Calls) != 1 {
		t.Fatalf("expected one call, got %q", recorder.Calls)
	}
	call := recorder.Calls[0]
	to := strings.TrimPrefix(call.Args[4], "--to=")
	if filepath.Dir(to) != cacheDir || !strings.HasPrefix(filepath.Base(to), "5.1.0-ec.0.tmp-") {
		t.Errorf("expected extraction into a temporary directory in the cache, got %s", call.Args[4])
	}
	want := processtest.Call{Path: "oc", Args: []string{"adm", "release", "extract",
		"--command=openshift-install",
		"--to=" + to,
		"--registry-config=/pull-secret.json",
		"quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
	}}
	if !reflect.DeepEqual(call, want) {
		t.Errorf("call:\ngot:  %q\nwant: %q", call, want)
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Errorf("expected the temporary directory to be gone, got %v", err)
	}
}

func TestCLIExtractInstallerFailureLeavesNoCacheEntry(t *testing.T) {
	cacheDir := t.TempDir()
	cli := CLI{runner: &processtest.Recorder{Results: []processtest.Result{
		{Stderr: "error: image not found", Err: errors.New("exit status 1")},
	}}}

	_, err := cli.ExtractInstaller(context.Background(), cacheDir, testRelease, "/pull-secret.json")

	if err == nil || !strings.Contains(err.Error(), "error: image not found") {
		t.Errorf("expected the oc output in the error, got %v", err)
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Errorf("expected an empty cache, got %v", entries)
	}
}

func TestCLICreateCluster(t *testing.T) {
	recorder := &processtest.Recorder{Results: []processtest.Result{{Stdout: "installing\n", Stderr: "level=info\n"}}}
	cli := CLI{runner: recorder}
	var out bytes.Buffer

	if err := cli.CreateCluster(context.Background(), Installer{Path: "/cache/openshift-install"}, "/install", testRelease, &out); err != nil {
		t.Fatal(err)
	}

	want := []processtest.Call{{
		Path: "/cache/openshift-install",
		Args: []string{"create", "cluster", "--dir", "/install"},
		Env: []string{
			"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE=quay.io/openshift-release-dev/ocp-release:5.1.0-ec.0-x86_64",
			"OPENSHIFT_INSTALL_EXPERIMENTAL_DISABLE_IMAGE_POLICY=true",
		},
	}}
	if !reflect.DeepEqual(recorder.Calls, want) {
		t.Errorf("calls:\ngot:  %q\nwant: %q", recorder.Calls, want)
	}
	if got, want := out.String(), "installing\nlevel=info\n"; got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

func TestCLIDestroyCluster(t *testing.T) {
	recorder := &processtest.Recorder{Results: []processtest.Result{{Stdout: "destroying\n"}}}
	cli := CLI{runner: recorder}
	var out bytes.Buffer

	if err := cli.DestroyCluster(context.Background(), Installer{Path: "/cache/openshift-install"}, "/install", &out); err != nil {
		t.Fatal(err)
	}

	want := []processtest.Call{{Path: "/cache/openshift-install", Args: []string{"destroy", "cluster", "--dir", "/install"}}}
	if !reflect.DeepEqual(recorder.Calls, want) {
		t.Errorf("calls:\ngot:  %q\nwant: %q", recorder.Calls, want)
	}
	if got, want := out.String(), "destroying\n"; got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

func TestCLIInstallerFailures(t *testing.T) {
	installer := Installer{Path: "/cache/openshift-install"}
	tests := map[string]struct {
		run  func(context.Context, CLI) error
		want string
	}{
		"create": {
			run: func(ctx context.Context, c CLI) error {
				return c.CreateCluster(ctx, installer, "/install", testRelease, &bytes.Buffer{})
			},
			want: "openshift-install create cluster: exit status 1",
		},
		"destroy": {
			run: func(ctx context.Context, c CLI) error {
				return c.DestroyCluster(ctx, installer, "/install", &bytes.Buffer{})
			},
			want: "openshift-install destroy cluster: exit status 1",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cli := CLI{runner: &processtest.Recorder{Results: []processtest.Result{{Err: errors.New("exit status 1")}}}}

			if err := tc.run(context.Background(), cli); err == nil || err.Error() != tc.want {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

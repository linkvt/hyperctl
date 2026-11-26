package app_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestKubeconfigPrintsKubeconfigPaths(t *testing.T) {
	tests := map[string]struct {
		args        []string
		wantServers []string
	}{
		"mgmt": {
			args:        []string{"kubeconfig", "mgmt"},
			wantServers: []string{"https://api.mgmt.fake:6443"},
		},
		"hosted": {
			args:        []string{"kubeconfig", "hosted"},
			wantServers: []string{"https://api.hosted.fake:6443"},
		},
		"both by default, hosted first": {
			args:        []string{"kubeconfig"},
			wantServers: []string{"https://api.hosted.fake:6443", "https://api.mgmt.fake:6443"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.createClusters(t)

			paths := strings.Split(strings.TrimSpace(env.run(t, tc.args...)), ":")

			if len(paths) != len(tc.wantServers) {
				t.Fatalf("got paths %v, want %d", paths, len(tc.wantServers))
			}
			for i, path := range paths {
				if got := readKubeconfig(t, path).Clusters[0].Cluster.Server; got != tc.wantServers[i] {
					t.Errorf("path %q: server %q, want %q", path, got, tc.wantServers[i])
				}
			}
		})
	}
}

func TestKubeconfigNamesEntriesAfterClusters(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	paths := strings.Split(strings.TrimSpace(env.run(t, "kubeconfig")), ":")
	// Running it again keeps the names.
	env.run(t, "kubeconfig")

	for i, name := range []string{"test-mgmt-workload", "test-mgmt"} {
		kubeconfig := readKubeconfig(t, paths[i])
		want := kubeconfigNames{
			cluster: name, context: name, contextCluster: name,
			user: name + "-admin", contextUser: name + "-admin", currentContext: name,
		}
		got := kubeconfigNames{
			cluster: kubeconfig.Clusters[0].Name, context: kubeconfig.Contexts[0].Name,
			contextCluster: kubeconfig.Contexts[0].Context.Cluster,
			user:           kubeconfig.Users[0].Name, contextUser: kubeconfig.Contexts[0].Context.User,
			currentContext: kubeconfig.CurrentContext,
		}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", paths[i], got, want)
		}
		if kubeconfig.Users[0].User.Token == "" || kubeconfig.Contexts[0].Context.Namespace != map[bool]string{true: "default", false: ""}[i == 0] {
			t.Errorf("%s: expected credentials and namespace to be kept: %+v", paths[i], kubeconfig)
		}
	}
}

func TestKubeconfigWithoutHostedClusterPrintsMgmt(t *testing.T) {
	env := newTestEnv(t)
	env.hypershift.installErr = errors.New("hypershift install: exit status 1")
	_, _ = env.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", env.pullSecret,
	)

	path := strings.TrimSpace(env.run(t, "kubeconfig"))

	if got := readKubeconfig(t, path).Clusters[0].Cluster.Server; got != "https://api.mgmt.fake:6443" {
		t.Errorf("got server %q, want the management cluster", got)
	}
}

func TestKubeconfigDuringHostedInstallation(t *testing.T) {
	tests := map[string]struct {
		cluster     string
		secret      string
		available   bool
		lookupError string
		wantServers []string
		wantError   string
	}{
		"hosted before installation finishes": {
			cluster: "hosted", secret: "workload-admin", available: true,
			wantServers: []string{"https://api.hosted.fake:6443"},
		},
		"both before installation finishes, hosted first": {
			secret: "workload-admin", available: true,
			wantServers: []string{"https://api.hosted.fake:6443", "https://api.mgmt.fake:6443"},
		},
		"default before secret reference exists": {
			wantServers: []string{"https://api.mgmt.fake:6443"},
		},
		"hosted before secret reference exists": {
			cluster: "hosted", wantError: "hosted kubeconfig not available yet",
		},
		"default before secret exists": {
			secret: "workload-admin", wantServers: []string{"https://api.mgmt.fake:6443"},
		},
		"hosted before secret exists": {
			cluster: "hosted", secret: "workload-admin", wantError: "hosted kubeconfig not available yet",
		},
		"default surfaces authorization failure": {
			lookupError: "Forbidden: cannot get hostedclusters", wantError: "Forbidden",
		},
		"hosted surfaces network failure": {
			cluster: "hosted", lookupError: "connection refused", wantError: "connection refused",
		},
		"mgmt never queries the cluster": {
			cluster: "mgmt", lookupError: "connection refused",
			wantServers: []string{"https://api.mgmt.fake:6443"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			dir := env.startHostedInstallation(t)
			recordPath := filepath.Join(dir, "cluster.yaml")
			record := readFile(t, recordPath)
			t.Setenv("HYPERCTL_TEST_HOSTED_KUBECONFIG_SECRET", tc.secret)
			t.Setenv("HYPERCTL_TEST_KUBECONFIG_ERROR", tc.lookupError)
			if tc.available {
				secretPath := filepath.Join(env.logDir, "hosted-kubeconfig")
				writeFile(t, secretPath, testHostedKubeconfig, 0o600)
				t.Setenv("HYPERCTL_TEST_HOSTED_KUBECONFIG", secretPath)
			}

			args := []string{"kubeconfig"}
			if tc.cluster != "" {
				args = append(args, tc.cluster)
			}
			out, err := env.runErr(t, args...)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected error containing %q, got %v", tc.wantError, err)
				}
				if out != "" {
					t.Errorf("expected no paths on failure, got %q", out)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				paths := strings.Split(strings.TrimSpace(out), string(os.PathListSeparator))
				if len(paths) != len(tc.wantServers) {
					t.Fatalf("got paths %v, want %d", paths, len(tc.wantServers))
				}
				for i, path := range paths {
					if got := readKubeconfig(t, path).Clusters[0].Cluster.Server; got != tc.wantServers[i] {
						t.Errorf("server: got %q, want %q", got, tc.wantServers[i])
					}
				}
			}
			if got := readFile(t, recordPath); got != record {
				t.Error("fetching kubeconfig changed the installation record")
			}
			if tc.cluster == "mgmt" {
				if calls := env.ocCalls(t, "get hostedcluster"); len(calls) != 0 {
					t.Errorf("mgmt queried the hosted cluster: %v", calls)
				}
			}
			if !tc.available {
				if _, err := os.Stat(filepath.Join(dir, "kubeconfig")); !os.IsNotExist(err) {
					t.Errorf("unexpected hosted kubeconfig file: %v", err)
				}
				return
			}

			path := filepath.Join(dir, "kubeconfig")
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("permissions: got %o, want 600", info.Mode().Perm())
			}
			if got := readKubeconfig(t, path); got.CurrentContext != "test-mgmt-workload" || got.Users[0].Name != "test-mgmt-workload-admin" {
				t.Error("hosted kubeconfig entries were not named after the cluster")
			}
			calls := env.ocCalls(t, "get secret")
			if len(calls) != 1 || !strings.Contains(calls[0], "get secret "+tc.secret+" -n clusters") {
				t.Errorf("expected retrieval of referenced secret, got %v", calls)
			}
			t.Setenv("HYPERCTL_TEST_KUBECONFIG_ERROR", "connection refused")
			if cached := env.run(t, args...); cached != out {
				t.Errorf("cached output: got %q, want %q", cached, out)
			}
			if calls := env.ocCalls(t, "get hostedcluster"); len(calls) != 1 {
				t.Errorf("cached kubeconfig queried the cluster: %v", calls)
			}
		})
	}
}

func TestKubeconfigCachedFilesWorkOffline(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)
	t.Setenv("HYPERCTL_TEST_KUBECONFIG_ERROR", "connection refused")

	env.run(t, "kubeconfig")
	env.run(t, "kubeconfig", "hosted")
	env.run(t, "kubeconfig", "mgmt")

	if calls := env.ocCalls(t, "get hostedcluster"); len(calls) != 0 {
		t.Errorf("cached kubeconfigs queried the cluster: %v", calls)
	}
}

type kubeconfigNames struct {
	cluster, context, contextCluster, user, contextUser, currentContext string
}

// kubeconfigFile is the part of a kubeconfig the tests check.
type kubeconfigFile struct {
	Clusters []struct {
		Name    string `json:"name"`
		Cluster struct {
			Server string `json:"server"`
		} `json:"cluster"`
	} `json:"clusters"`
	Contexts []struct {
		Name    string `json:"name"`
		Context struct {
			Cluster   string `json:"cluster"`
			Namespace string `json:"namespace"`
			User      string `json:"user"`
		} `json:"context"`
	} `json:"contexts"`
	CurrentContext string `json:"current-context"`
	Users          []struct {
		Name string `json:"name"`
		User struct {
			Token string `json:"token"`
		} `json:"user"`
	} `json:"users"`
}

func (e *testEnv) startHostedInstallation(t *testing.T) string {
	t.Helper()
	e.hypershift.createClusterErr = errors.New("hypershift create cluster: exit status 1")
	if _, err := e.runErr(t, "create",
		"--release", "5.1.0-ec.0",
		"--region", "eu-central-1",
		"--name", "test-mgmt",
		"--pull-secret", e.pullSecret,
	); err == nil {
		t.Fatal("expected incomplete hosted installation")
	}
	return filepath.Join(e.home, ".local", "share", "hyperctl", "clusters", "test-mgmt", "hosted", "test-mgmt-workload")
}

func readKubeconfig(t *testing.T, path string) kubeconfigFile {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading kubeconfig %q: %v", path, err)
	}
	var k kubeconfigFile
	if err := yaml.Unmarshal(b, &k); err != nil {
		t.Fatalf("parsing kubeconfig %q: %v", path, err)
	}
	if len(k.Clusters) != 1 || len(k.Contexts) != 1 || len(k.Users) != 1 {
		t.Fatalf("expected one cluster, context and user in %q:\n%s", path, b)
	}
	return k
}

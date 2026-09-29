package app_test

import (
	"os"
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
	t.Setenv("HYPERCTL_TEST_HYPERSHIFT_FAIL", "install")
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

package app_test

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"testing"

	"github.com/linkvt/hyperctl/internal/app"
	"github.com/linkvt/hyperctl/internal/hypershift"
)

// testHypershiftPath is the hypershift binary in PATH.
const testHypershiftPath = "/usr/local/bin/hypershift"

// fakeHypershift is an in-memory hypershift binary. It records the
// operations it ran, with the binary and management cluster they ran with,
// and creates and destroys the hosted clusters of the fake cluster.
type fakeHypershift struct {
	cluster *fakeCluster
	// path is the hypershift binary in PATH, none if empty.
	path string
	// binaries are the paths of working hypershift binaries.
	binaries []string
	// installErr and createClusterErr fail Install and CreateCluster.
	installErr       error
	createClusterErr error

	installs      []recorded[hypershift.InstallParams]
	azureInstalls []recorded[hypershift.InstallAzureParams]
	clusters      []recorded[hypershift.CreateClusterParams]
	destroys      []recorded[hypershift.DestroyClusterParams]
	infraDestroys []recorded[hypershift.DestroyClusterParams]
	azureIAMs     []recorded[hypershift.CreateAzureIAMParams]
	azureInfras   []recorded[hypershift.CreateAzureInfraParams]
	azureClusters []recorded[hypershift.CreateAzureClusterParams]
}

// recorded is an operation of the fake hypershift with its parameters.
type recorded[P any] struct {
	path       string
	kubeconfig string
	params     P
}

// fakeHypershiftCLI runs the operations of a fakeHypershift. Like the real
// binary, they write their output to out.
type fakeHypershiftCLI struct {
	fake       *fakeHypershift
	path       string
	kubeconfig string
	out        io.Writer
}

func newFakeHypershift(cluster *fakeCluster) *fakeHypershift {
	return &fakeHypershift{
		cluster:  cluster,
		path:     testHypershiftPath,
		binaries: []string{testHypershiftPath},
	}
}

// lookPath finds the hypershift binary in PATH.
func (f *fakeHypershift) lookPath(file string) (string, error) {
	if file != "hypershift" || f.path == "" {
		return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
	}
	return f.path, nil
}

// cli returns the hypershift binary at path.
func (f *fakeHypershift) cli(path, kubeconfig string, out io.Writer) app.Hypershift {
	return &fakeHypershiftCLI{fake: f, path: path, kubeconfig: kubeconfig, out: out}
}

func (c *fakeHypershiftCLI) Version(context.Context) error {
	if !slices.Contains(c.fake.binaries, c.path) {
		return fmt.Errorf("hypershift binary %s does not run", c.path)
	}
	return nil
}

func (c *fakeHypershiftCLI) Install(_ context.Context, params hypershift.InstallParams) error {
	c.fake.installs = append(c.fake.installs, record(c, params))
	if c.fake.installErr != nil {
		return c.fake.installErr
	}
	c.print("install")
	return nil
}

func (c *fakeHypershiftCLI) InstallAzure(_ context.Context, params hypershift.InstallAzureParams) error {
	c.fake.azureInstalls = append(c.fake.azureInstalls, record(c, params))
	c.print("install")
	return nil
}

func (c *fakeHypershiftCLI) CreateCluster(_ context.Context, params hypershift.CreateClusterParams) error {
	c.fake.clusters = append(c.fake.clusters, record(c, params))
	if c.fake.createClusterErr != nil {
		// The HostedCluster exists, but does not become available.
		c.fake.cluster.hostedClusters[params.Name] = &fakeHostedCluster{available: "False"}
		return c.fake.createClusterErr
	}
	c.fake.cluster.hostedClusters[params.Name] = availableHostedCluster()
	c.print("create")
	return nil
}

func (c *fakeHypershiftCLI) DestroyCluster(_ context.Context, params hypershift.DestroyClusterParams) error {
	c.fake.destroys = append(c.fake.destroys, record(c, params))
	delete(c.fake.cluster.hostedClusters, params.Name)
	c.print("destroy")
	return nil
}

func (c *fakeHypershiftCLI) DestroyInfra(_ context.Context, params hypershift.DestroyClusterParams) error {
	c.fake.infraDestroys = append(c.fake.infraDestroys, record(c, params))
	c.print("destroy")
	return nil
}

func (c *fakeHypershiftCLI) HostedKubeconfig(_ context.Context, name string) ([]byte, error) {
	hostedCluster, found := c.fake.cluster.hostedClusters[name]
	if !found || hostedCluster.kubeconfig == nil {
		return nil, fmt.Errorf("hypershift create kubeconfig: no kubeconfig for hosted cluster %s", name)
	}
	return hostedCluster.kubeconfig, nil
}

func (c *fakeHypershiftCLI) CreateAzureIAM(_ context.Context, params hypershift.CreateAzureIAMParams) error {
	c.fake.azureIAMs = append(c.fake.azureIAMs, record(c, params))
	c.print("create")
	return nil
}

func (c *fakeHypershiftCLI) CreateAzureInfra(_ context.Context, params hypershift.CreateAzureInfraParams) error {
	c.fake.azureInfras = append(c.fake.azureInfras, record(c, params))
	c.print("create")
	return nil
}

func (c *fakeHypershiftCLI) CreateAzureCluster(_ context.Context, params hypershift.CreateAzureClusterParams) error {
	c.fake.azureClusters = append(c.fake.azureClusters, record(c, params))
	c.fake.cluster.hostedClusters[params.Name] = availableHostedCluster()
	c.print("create")
	return nil
}

// print writes the output of a hypershift subcommand.
func (c *fakeHypershiftCLI) print(subcommand string) {
	_, _ = fmt.Fprintf(c.out, "fake %s output\n", subcommand)
}

func record[P any](c *fakeHypershiftCLI, params P) recorded[P] {
	return recorded[P]{path: c.path, kubeconfig: c.kubeconfig, params: params}
}

// single returns the only recorded operation and fails the test unless
// there is exactly one.
func single[P any](t *testing.T, calls []recorded[P]) recorded[P] {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %+v", calls)
	}
	return calls[0]
}

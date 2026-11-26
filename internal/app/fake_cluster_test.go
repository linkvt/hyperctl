package app_test

import (
	"context"
	"fmt"
)

// fakeCluster is the in-memory API of the management cluster. The fake
// hypershift creates and destroys its hosted clusters.
type fakeCluster struct {
	apiDown bool
	// hostedKubeconfigErr fails looking up the kubeconfig of hosted clusters.
	hostedKubeconfigErr error

	hostedClusters map[string]*fakeHostedCluster
	annotations    map[fakeObject]map[string]string

	hostedKubeconfigLookups int
	appliedURLs             []string
	deletedPods             []fakePods
}

// fakeHostedCluster is a HostedCluster in the clusters namespace.
type fakeHostedCluster struct {
	// available is the status of the Available condition.
	available string
	// kubeconfig is the admin kubeconfig, nil until hypershift publishes it.
	kubeconfig []byte
}

// fakeObject identifies a namespaced object.
type fakeObject struct {
	kind      string
	namespace string
	name      string
}

// fakePods are the pods matching a label selector in a namespace.
type fakePods struct {
	namespace string
	selector  string
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{
		hostedClusters: map[string]*fakeHostedCluster{},
		annotations:    map[fakeObject]map[string]string{},
	}
}

func (f *fakeCluster) Ready(context.Context, string) bool {
	return !f.apiDown
}

func (f *fakeCluster) HostedClusterAvailable(_ context.Context, _, name string) (string, error) {
	hostedCluster, found := f.hostedClusters[name]
	if !found {
		return "", fmt.Errorf("getting hosted cluster %s: not found", name)
	}
	return hostedCluster.available, nil
}

func (f *fakeCluster) HostedKubeconfig(_ context.Context, _, name string) ([]byte, error) {
	f.hostedKubeconfigLookups++
	if f.hostedKubeconfigErr != nil {
		return nil, f.hostedKubeconfigErr
	}
	hostedCluster, found := f.hostedClusters[name]
	if !found {
		return nil, nil
	}
	return hostedCluster.kubeconfig, nil
}

func (f *fakeCluster) Annotations(_ context.Context, _, kind, namespace, name string) (map[string]string, error) {
	return f.annotations[fakeObject{kind: kind, namespace: namespace, name: name}], nil
}

func (f *fakeCluster) Annotate(_ context.Context, _, kind, namespace, name, key, value string) error {
	object := fakeObject{kind: kind, namespace: namespace, name: name}
	if f.annotations[object] == nil {
		f.annotations[object] = map[string]string{}
	}
	f.annotations[object][key] = value
	return nil
}

func (f *fakeCluster) RemoveAnnotation(_ context.Context, _, kind, namespace, name, key string) error {
	delete(f.annotations[fakeObject{kind: kind, namespace: namespace, name: name}], key)
	return nil
}

func (f *fakeCluster) DeletePods(_ context.Context, _, namespace, selector string) error {
	f.deletedPods = append(f.deletedPods, fakePods{namespace: namespace, selector: selector})
	return nil
}

func (f *fakeCluster) ApplyServerSide(_ context.Context, _, url string) error {
	f.appliedURLs = append(f.appliedURLs, url)
	return nil
}

// availableHostedCluster is a created hosted cluster with its kubeconfig.
func availableHostedCluster() *fakeHostedCluster {
	return &fakeHostedCluster{available: "True", kubeconfig: []byte(testHostedKubeconfig)}
}

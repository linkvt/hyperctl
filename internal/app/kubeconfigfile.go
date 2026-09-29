package app

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// nameKubeconfig names the cluster, context and user of a kubeconfig file
// after the cluster, so that kubeconfigs of several clusters can be merged.
// The installer and hypershift both name their context and user "admin",
// which would collide. Kubeconfigs with more than one cluster, context or
// user are left untouched.
func nameKubeconfig(path, name string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var kubeconfig map[string]any
	if err := yaml.Unmarshal(b, &kubeconfig); err != nil {
		return fmt.Errorf("parsing kubeconfig %s: %w", path, err)
	}
	cluster, clusterOK := singleEntry(kubeconfig, "clusters")
	context, contextOK := singleEntry(kubeconfig, "contexts")
	user, userOK := singleEntry(kubeconfig, "users")
	if !clusterOK || !contextOK || !userOK {
		return nil
	}

	userName := name + "-admin"
	cluster["name"] = name
	context["name"] = name
	user["name"] = userName
	if contextFields, ok := context["context"].(map[string]any); ok {
		contextFields["cluster"] = name
		contextFields["user"] = userName
	}
	kubeconfig["current-context"] = name

	b, err = yaml.Marshal(kubeconfig)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// singleEntry returns the only entry of a kubeconfig list.
func singleEntry(kubeconfig map[string]any, list string) (map[string]any, bool) {
	entries, ok := kubeconfig[list].([]any)
	if !ok || len(entries) != 1 {
		return nil, false
	}
	entry, ok := entries[0].(map[string]any)
	return entry, ok
}

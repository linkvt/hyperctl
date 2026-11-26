// Package oc queries clusters with the oc CLI.
package oc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Ready reports whether the API server of the cluster responds.
func Ready(ctx context.Context, kubeconfig string) bool {
	cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfig, "get", "--raw", "/readyz", "--request-timeout=10s")
	return cmd.Run() == nil
}

// HostedClusterAvailable returns the status of the Available condition of a
// HostedCluster in the clusters namespace.
func HostedClusterAvailable(ctx context.Context, kubeconfig, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfig,
		"get", "hostedcluster", name, "-n", "clusters",
		"-o", `jsonpath={.status.conditions[?(@.type=="Available")].status}`,
		"--request-timeout=10s")
	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return "", fmt.Errorf("getting hosted cluster %s: %w: %s", name, err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	if err != nil {
		return "", fmt.Errorf("getting hosted cluster %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Annotations returns the annotations of a namespaced object.
func Annotations(ctx context.Context, kubeconfig, kind, namespace, name string) (map[string]string, error) {
	out, err := run(ctx, kubeconfig, "get", kind, name, "-n", namespace, "--request-timeout=10s", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("getting %s %s: %w", kind, name, err)
	}
	var object struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &object); err != nil {
		return nil, fmt.Errorf("parsing %s %s: %w", kind, name, err)
	}
	return object.Metadata.Annotations, nil
}

// Annotate sets an annotation of a namespaced object.
func Annotate(ctx context.Context, kubeconfig, kind, namespace, name, key, value string) error {
	if _, err := run(ctx, kubeconfig, "annotate", kind, name, "-n", namespace, "--overwrite", key+"="+value); err != nil {
		return fmt.Errorf("annotating %s %s: %w", kind, name, err)
	}
	return nil
}

// RemoveAnnotation removes an annotation of a namespaced object.
func RemoveAnnotation(ctx context.Context, kubeconfig, kind, namespace, name, key string) error {
	if _, err := run(ctx, kubeconfig, "annotate", kind, name, "-n", namespace, key+"-"); err != nil {
		return fmt.Errorf("removing annotation of %s %s: %w", kind, name, err)
	}
	return nil
}

// DeletePods deletes the pods matching a label selector in a namespace.
func DeletePods(ctx context.Context, kubeconfig, namespace, selector string) error {
	if _, err := run(ctx, kubeconfig, "delete", "pod", "-n", namespace, "-l", selector); err != nil {
		return fmt.Errorf("deleting pods %s in %s: %w", selector, namespace, err)
	}
	return nil
}

// ApplyServerSide applies the manifests at a URL with server-side apply.
func ApplyServerSide(ctx context.Context, kubeconfig, url string) error {
	if _, err := run(ctx, kubeconfig, "apply", "--server-side", "-f", url); err != nil {
		return fmt.Errorf("applying %s: %w", url, err)
	}
	return nil
}

// DeleteHostedCluster deletes a HostedCluster in the clusters namespace and
// waits until its control plane namespace is gone.
func DeleteHostedCluster(ctx context.Context, kubeconfig, name, controlPlaneNamespace string) error {
	if _, err := run(ctx, kubeconfig, "delete", "hostedcluster", name, "-n", "clusters", "--ignore-not-found", "--timeout=30m"); err != nil {
		return fmt.Errorf("deleting hosted cluster %s: %w", name, err)
	}
	return waitNamespaceDeleted(ctx, kubeconfig, controlPlaneNamespace)
}

// waitNamespaceDeleted waits until the namespace is gone. A namespace that
// was already deleted counts as gone, which waiting for it does not report
// consistently.
func waitNamespaceDeleted(ctx context.Context, kubeconfig, namespace string) error {
	_, waitErr := run(ctx, kubeconfig, "wait", "--for=delete", "namespace/"+namespace, "--timeout=15m")
	if waitErr == nil {
		return nil
	}
	out, err := run(ctx, kubeconfig, "get", "namespace", namespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		return fmt.Errorf("checking namespace %s: %w", namespace, err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("waiting for namespace %s to be deleted: %w", namespace, waitErr)
	}
	return nil
}

// run runs oc against the cluster of kubeconfig and returns its output,
// with stderr in the error.
func run(ctx context.Context, kubeconfig string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "oc", append([]string{"--kubeconfig", kubeconfig}, args...)...)
	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return out, err
}

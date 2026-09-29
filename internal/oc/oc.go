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

// HostedClusterAnnotations returns the annotations of a HostedCluster in the
// clusters namespace.
func HostedClusterAnnotations(ctx context.Context, kubeconfig, name string) (map[string]string, error) {
	out, err := run(ctx, kubeconfig, "get", "hostedcluster", name, "-n", "clusters", "--request-timeout=10s", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("getting hosted cluster %s: %w", name, err)
	}
	var hostedCluster struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &hostedCluster); err != nil {
		return nil, fmt.Errorf("parsing hosted cluster %s: %w", name, err)
	}
	return hostedCluster.Metadata.Annotations, nil
}

// AnnotateHostedCluster sets an annotation of a HostedCluster in the clusters
// namespace.
func AnnotateHostedCluster(ctx context.Context, kubeconfig, name, key, value string) error {
	if _, err := run(ctx, kubeconfig, "annotate", "hostedcluster", name, "-n", "clusters", "--overwrite", key+"="+value); err != nil {
		return fmt.Errorf("annotating hosted cluster %s: %w", name, err)
	}
	return nil
}

// RemoveHostedClusterAnnotation removes an annotation of a HostedCluster in
// the clusters namespace.
func RemoveHostedClusterAnnotation(ctx context.Context, kubeconfig, name, key string) error {
	if _, err := run(ctx, kubeconfig, "annotate", "hostedcluster", name, "-n", "clusters", key+"-"); err != nil {
		return fmt.Errorf("removing annotation of hosted cluster %s: %w", name, err)
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

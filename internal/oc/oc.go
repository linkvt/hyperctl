// Package oc queries clusters with the oc CLI.
package oc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/linkvt/hyperctl/internal/process"
)

// CLI runs oc against clusters.
type CLI struct {
	runner process.Runner
}

// New returns a CLI running the oc binary in PATH.
func New() CLI {
	return CLI{runner: process.Exec{}}
}

// Ready reports whether the API server of the cluster responds.
func (c CLI) Ready(ctx context.Context, kubeconfig string) bool {
	_, err := c.run(ctx, kubeconfig, "get", "--raw", "/readyz", "--request-timeout=10s")
	return err == nil
}

// HostedClusterAvailable returns the status of the Available condition of a
// HostedCluster in the clusters namespace.
func (c CLI) HostedClusterAvailable(ctx context.Context, kubeconfig, name string) (string, error) {
	out, err := c.run(ctx, kubeconfig,
		"get", "hostedcluster", name, "-n", "clusters",
		"-o", `jsonpath={.status.conditions[?(@.type=="Available")].status}`,
		"--request-timeout=10s")
	if err != nil {
		return "", fmt.Errorf("getting hosted cluster %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Annotations returns the annotations of a namespaced object.
func (c CLI) Annotations(ctx context.Context, kubeconfig, kind, namespace, name string) (map[string]string, error) {
	out, err := c.run(ctx, kubeconfig, "get", kind, name, "-n", namespace, "--request-timeout=10s", "-o", "json")
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
func (c CLI) Annotate(ctx context.Context, kubeconfig, kind, namespace, name, key, value string) error {
	if _, err := c.run(ctx, kubeconfig, "annotate", kind, name, "-n", namespace, "--overwrite", key+"="+value); err != nil {
		return fmt.Errorf("annotating %s %s: %w", kind, name, err)
	}
	return nil
}

// RemoveAnnotation removes an annotation of a namespaced object.
func (c CLI) RemoveAnnotation(ctx context.Context, kubeconfig, kind, namespace, name, key string) error {
	if _, err := c.run(ctx, kubeconfig, "annotate", kind, name, "-n", namespace, key+"-"); err != nil {
		return fmt.Errorf("removing annotation of %s %s: %w", kind, name, err)
	}
	return nil
}

// DeletePods deletes the pods matching a label selector in a namespace.
func (c CLI) DeletePods(ctx context.Context, kubeconfig, namespace, selector string) error {
	if _, err := c.run(ctx, kubeconfig, "delete", "pod", "-n", namespace, "-l", selector); err != nil {
		return fmt.Errorf("deleting pods %s in %s: %w", selector, namespace, err)
	}
	return nil
}

// ApplyServerSide applies the manifests at a URL with server-side apply.
func (c CLI) ApplyServerSide(ctx context.Context, kubeconfig, url string) error {
	if _, err := c.run(ctx, kubeconfig, "apply", "--server-side", "-f", url); err != nil {
		return fmt.Errorf("applying %s: %w", url, err)
	}
	return nil
}

// run runs oc against the cluster of kubeconfig and returns its output,
// with stderr in the error.
func (c CLI) run(ctx context.Context, kubeconfig string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	err := c.runner.Run(ctx, process.Command{
		Path:   "oc",
		Args:   append([]string{"--kubeconfig", kubeconfig}, args...),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

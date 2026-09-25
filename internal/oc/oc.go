// Package oc queries clusters with the oc CLI.
package oc

import (
	"context"
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
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "", fmt.Errorf("getting hosted cluster %s: %w: %s", name, err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	if err != nil {
		return "", fmt.Errorf("getting hosted cluster %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

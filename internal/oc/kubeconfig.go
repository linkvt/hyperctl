package oc

import (
	"context"
	"encoding/json"
	"fmt"
)

// HostedKubeconfig returns the hosted cluster's admin kubeconfig, or nil if
// its reference or secret does not exist yet.
func (c CLI) HostedKubeconfig(ctx context.Context, kubeconfig, name string) ([]byte, error) {
	out, err := c.run(ctx, kubeconfig, "get", "hostedcluster", name, "-n", "clusters",
		"--ignore-not-found", "--request-timeout=10s", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("getting hosted cluster %s: %w", name, err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	var hostedCluster struct {
		Status struct {
			Kubeconfig struct {
				Name string `json:"name"`
			} `json:"kubeconfig"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &hostedCluster); err != nil {
		return nil, fmt.Errorf("parsing hosted cluster %s: %w", name, err)
	}
	secretName := hostedCluster.Status.Kubeconfig.Name
	if secretName == "" {
		return nil, nil
	}
	out, err = c.run(ctx, kubeconfig, "get", "secret", secretName, "-n", "clusters",
		"--ignore-not-found", "--request-timeout=10s", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("getting hosted kubeconfig secret %s: %w", secretName, err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	var secret struct {
		Data struct {
			Kubeconfig []byte `json:"kubeconfig"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &secret); err != nil {
		return nil, fmt.Errorf("parsing hosted kubeconfig secret %s: %w", secretName, err)
	}
	if len(secret.Data.Kubeconfig) == 0 {
		return nil, fmt.Errorf("hosted kubeconfig secret %s has no kubeconfig", secretName)
	}
	return secret.Data.Kubeconfig, nil
}

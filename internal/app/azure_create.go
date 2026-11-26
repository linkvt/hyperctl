package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/azureaccount"
	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/xdg"
)

const (
	defaultAzureBaseDomain = "azure.autoscaling-dev.devcluster.openshift.com"
	defaultAzureVMSize     = "Standard_D4s_v5"
	defaultAzureLocation   = "westus3"
	defaultAKSNodes        = 3
	// aksZones is the number of zones the AKS nodes are spread across.
	aksZones = 2
)

// hypershiftCRDs are the CRDs the HyperShift operator needs that an AKS
// cluster lacks.
var hypershiftCRDs = []string{
	"https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml",
	"https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml",
	"https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/example/prometheus-operator-crd/monitoring.coreos.com_podmonitors.yaml",
	"https://raw.githubusercontent.com/openshift/api/6bababe9164ea6c78274fd79c94a3f951f8d5ab2/route/v1/zz_generated.crd-manifests/routes.crd.yaml",
}

// runCreateAzure creates an AKS management cluster and an Azure hosted
// cluster, together with the Azure resources they share with later clusters.
func runCreateAzure(ctx context.Context, p progress, cmd *cli.Command, deps Deps, dirs xdg.Dirs, st state, cfg config) error {
	var subscription azureaccount.Subscription
	err := p.wait(ctx, "Checking the Azure login", func(ctx context.Context) error {
		var err error
		subscription, err = deps.Azure.Subscription(ctx)
		return err
	})
	if err != nil {
		return err
	}
	p.done("Azure subscription %s (%s)", subscription.Name, subscription.ID)
	hypershiftPath, err := resolveHypershift(ctx, cmd, deps, dirs, cfg)
	if err != nil {
		return err
	}
	p.done("hypershift %s", hypershiftPath)

	opts, err := resolveCreateOptions(ctx, p, cmd, deps, dirs, st, cfg, platformAzure)
	if err != nil {
		return err
	}
	if !opts.StandaloneKarpenterOperator {
		return errors.New("AutoNode on Azure requires the standalone karpenter-operator, remove --standalone-karpenter-operator=false")
	}
	if err := requireTrackedClusters(st, opts); err != nil {
		return err
	}
	st.LastRelease = opts.Release
	st.ManagementOnly = opts.ManagementOnly
	if err := saveState(dirs.State, st); err != nil {
		return err
	}
	// Reload the config, resolving the options may have saved it.
	if cfg, err = loadConfig(dirs.Config); err != nil {
		return err
	}
	cfg.Azure.Location = opts.Region
	if err := saveConfig(dirs.Config, cfg); err != nil {
		return err
	}

	p.step("Checking the Azure subscription")
	var zones []string
	err = p.wait(ctx, "Checking "+opts.InstanceType+" in "+opts.Region, func(ctx context.Context) error {
		var err error
		zones, err = deps.Azure.AvailableZones(ctx, opts.Region, opts.InstanceType)
		return err
	})
	if err != nil {
		return err
	}
	zones = zones[:min(len(zones), aksZones)]
	p.done("%s is available in %s (zones: %s)", opts.InstanceType, opts.Region, strings.Join(zones, ", "))

	p.step("Shared Azure resources")
	shared, err := newAzureShared(dirs, cfg, subscription, opts.BaseDomain, opts.Region)
	if err != nil {
		return err
	}
	if err := ensureAzureShared(ctx, p, deps.Azure, shared); err != nil {
		return err
	}

	// Track the clusters before creating them, so that incomplete ones can
	// be destroyed.
	st.MgmtCluster = opts.Name
	if err := saveState(dirs.State, st); err != nil {
		return err
	}
	p.step("Management cluster %s", opts.Name)
	if err := ensureAKSCluster(ctx, p, deps.Azure, dirs, opts, zones); err != nil {
		return err
	}

	p.step("HyperShift operator")
	kubeconfig := mgmtKubeconfigPath(dirs, opts.Name)
	hs := deps.Hypershift(hypershiftPath, kubeconfig, p.tool("hypershift"))
	if err := installAzureOperator(ctx, p, deps.Cluster, hs, kubeconfig, opts, shared); err != nil {
		return err
	}

	if !opts.ManagementOnly {
		st.HostedCluster = opts.HostedName
		if err := saveState(dirs.State, st); err != nil {
			return err
		}
		p.step("Hosted cluster %s", opts.HostedName)
		if err := ensureAzureHostedCluster(ctx, p, deps.Azure, hs, dirs, opts, shared); err != nil {
			return err
		}
	}

	p.step("Done, use the cluster kubeconfig with:")
	p.info("export KUBECONFIG=$(hyperctl kubeconfig)")
	return nil
}

// installAzureOperator installs the HyperShift operator with External DNS,
// after adding the CRDs it expects from an OpenShift cluster.
func installAzureOperator(ctx context.Context, p progress, cluster ClusterAPI, hs Hypershift, kubeconfig string, opts createOptions, shared azureShared) error {
	err := p.wait(ctx, "Applying the Prometheus and Route CRDs", func(ctx context.Context) error {
		for _, url := range hypershiftCRDs {
			if err := cluster.ApplyServerSide(ctx, kubeconfig, url); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.done("Applied the Prometheus and Route CRDs")

	start := time.Now()
	if err := hs.InstallAzure(ctx, hypershift.InstallAzureParams{
		PullSecret:                 opts.PullSecret,
		ExternalDNSCredentialsFile: shared.ExternalDNSCredentialsFile,
		ExternalDNSDomain:          shared.DNSZone,
	}); err != nil {
		return err
	}
	p.done("Installed the HyperShift operator (%s)", since(start))
	return nil
}

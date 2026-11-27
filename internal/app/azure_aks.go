package app

import (
	"context"
	"fmt"
	"slices"
	"time"

	"charm.land/huh/v2"

	"github.com/linkvt/hyperctl/internal/azureaccount"
)

const (
	defaultAzureVMSize = "Standard_D4s_v5"
	defaultAKSNodes    = 3
	// aksZones is the number of zones the AKS nodes are spread across.
	aksZones = 2
)

// aksSize is the size of an AKS management cluster.
type aksSize struct {
	VMSize string
	Nodes  int
}

// aksSizePresets are the sizes offered in the prompt, the first one as in
// hypershift's contrib/managed-azure/setup_aks_cluster.sh.
var aksSizePresets = []aksSize{{defaultAzureVMSize, defaultAKSNodes}}

// ensureAKSCluster creates the AKS management cluster unless it is
// installed, tracking progress in the cluster record.
func ensureAKSCluster(ctx context.Context, p progress, account AzureAccount, clusters clusterStore, opts createOptions, size aksSize, zones []string) error {
	c := managementCluster(opts.Name)
	installed, err := clusters.check(c)
	if err != nil {
		return err
	}
	if installed {
		p.done("%s %s is already installed", opts.Setup.clusterTitle(), opts.Name)
		return nil
	}

	if err := clusters.begin(c, clusterRecord{
		Platform:   platformAzure,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
		PullSecret: opts.PullSecret,
	}); err != nil {
		return err
	}
	resourceGroup := aksResourceGroup(opts.Name)
	if _, err := account.EnsureResourceGroup(ctx, resourceGroup, opts.Region); err != nil {
		return err
	}
	start := time.Now()
	err = p.wait(ctx, "Creating AKS cluster "+opts.Name+" in resource group "+resourceGroup+", this takes about 10 minutes", func(ctx context.Context) error {
		return account.CreateAKSCluster(ctx, azureaccount.AKSClusterParams{
			ResourceGroup: resourceGroup,
			Name:          opts.Name,
			Location:      opts.Region,
			VMSize:        size.VMSize,
			Nodes:         size.Nodes,
			Zones:         zones,
		})
	})
	if err != nil {
		return err
	}
	kubeconfig, err := account.AKSAdminKubeconfig(ctx, resourceGroup, opts.Name)
	if err != nil {
		return err
	}
	if err := writeFile(clusters.kubeconfig(c), kubeconfig); err != nil {
		return err
	}
	if err := clusters.markInstalled(c); err != nil {
		return err
	}
	p.done("Installed %s %s (%s)", opts.Setup.clusterNoun(), opts.Name, since(start))
	return nil
}

// describe describes the size for the summary.
func (s aksSize) describe() string {
	return fmt.Sprintf("AKS with %d nodes %s", s.Nodes, s.VMSize)
}

// promptAKSSize lets the user pick an AKS cluster size.
func promptAKSSize(ctx context.Context, title string, size *aksSize) error {
	var options []huh.Option[aksSize]
	for _, s := range aksSizeChoices(*size) {
		options = append(options, huh.NewOption(fmt.Sprintf("%d nodes %s", s.Nodes, s.VMSize), s))
	}
	return huh.NewForm(huh.NewGroup(
		huh.NewSelect[aksSize]().Title(title).Options(options...).Value(size),
	)).RunWithContext(ctx)
}

// aksSizeChoices returns the presets and the current size, e.g. from the
// config file, unless it is a preset.
func aksSizeChoices(current aksSize) []aksSize {
	choices := slices.Clone(aksSizePresets)
	if !slices.Contains(choices, current) {
		choices = append(choices, current)
	}
	return choices
}

// aksResourceGroup holds the AKS cluster of a management cluster.
func aksResourceGroup(name string) string {
	return name + "-rg"
}

// destroyAKSCluster deletes the resource group of the AKS management cluster
// and the node resource group Azure manages for it.
func destroyAKSCluster(ctx context.Context, p progress, account AzureAccount, clusters clusterStore, name string) error {
	c := managementCluster(name)
	record, found, err := clusters.destroying(c)
	if err != nil {
		return err
	}
	if !found {
		p.done("Installation never started, nothing to destroy")
		return nil
	}
	resourceGroup := aksResourceGroup(name)
	// Azure deletes the node resource group with the cluster, deleting it
	// again covers a deletion that did not complete.
	nodeResourceGroup := fmt.Sprintf("MC_%s_%s_%s", resourceGroup, name, record.Region)
	start := time.Now()
	err = p.wait(ctx, "Deleting resource groups "+resourceGroup+" and "+nodeResourceGroup, func(ctx context.Context) error {
		if err := account.DeleteResourceGroups(ctx, resourceGroup); err != nil {
			return err
		}
		return account.DeleteResourceGroups(ctx, nodeResourceGroup)
	})
	if err != nil {
		return err
	}
	p.done("Destroyed management cluster %s (%s)", name, since(start))
	return clusters.forget(c)
}

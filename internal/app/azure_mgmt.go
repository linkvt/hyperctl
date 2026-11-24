package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// ensureAKSCluster creates the AKS management cluster unless it is
// installed, tracking progress in the cluster record.
func ensureAKSCluster(ctx context.Context, p progress, account AzureAccount, dirs xdg.Dirs, opts createOptions, zones []string) error {
	dir := clusterDir(dirs, opts.Name)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	switch {
	case found && record.Phase == phaseInstalled:
		p.done("Management cluster %s is already installed", opts.Name)
		return nil
	case found:
		return fmt.Errorf("previous installation of management cluster %s did not complete, destroy it first", opts.Name)
	}

	record = clusterRecord{
		Platform:   platformAzure,
		Phase:      phaseInstalling,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
		PullSecret: opts.PullSecret,
	}
	if err := saveClusterRecord(dir, record); err != nil {
		return err
	}
	resourceGroup := aksResourceGroup(opts.Name)
	if _, err := account.EnsureResourceGroup(ctx, resourceGroup, opts.Region); err != nil {
		return err
	}
	start := time.Now()
	err = p.wait(ctx, "Creating AKS cluster "+opts.Name+" in resource group "+resourceGroup+", this takes about 10 minutes", func(ctx context.Context) error {
		return account.CreateAKSCluster(ctx, AKSClusterParams{
			ResourceGroup: resourceGroup,
			Name:          opts.Name,
			Location:      opts.Region,
			VMSize:        opts.InstanceType,
			Nodes:         opts.Workers,
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
	if err := writeFile(mgmtKubeconfigPath(dirs, opts.Name), kubeconfig); err != nil {
		return err
	}
	record.Phase = phaseInstalled
	if err := saveClusterRecord(dir, record); err != nil {
		return err
	}
	p.done("Installed management cluster %s (%s)", opts.Name, since(start))
	return nil
}

// aksResourceGroup holds the AKS cluster of a management cluster.
func aksResourceGroup(name string) string {
	return name + "-rg"
}

// destroyAKSCluster deletes the resource group of the AKS management cluster
// and the node resource group Azure manages for it.
func destroyAKSCluster(ctx context.Context, p progress, account AzureAccount, dirs xdg.Dirs, name string) error {
	dir := clusterDir(dirs, name)
	record, found, err := loadClusterRecord(dir)
	if err != nil {
		return err
	}
	if !found {
		p.done("Installation never started, nothing to destroy")
		return os.RemoveAll(dir)
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
	return os.RemoveAll(dir)
}

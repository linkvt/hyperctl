# Azure

On Azure, hyperctl creates an AKS cluster and hosted clusters with `hypershift`.

## Prerequisites

- The Azure CLI `az`, logged in with `az login`. hyperctl uses its credentials and its default subscription (`az account set` changes it). The login needs permission to create resource groups and AKS clusters, e.g. Contributor.
- With HyperShift:
  - Also permission to create role assignments on the subscription, e.g. Owner, and app registrations in Entra ID.
  - A public Azure DNS zone for the base domain, by default `azure.autoscaling-dev.devcluster.openshift.com` in resource group `os4-common`.
  - A pull secret.

Azure AutoNode requires the standalone karpenter-operator, so `--standalone-karpenter-operator=false` is rejected.

## Shared resources

With HyperShift, `create` creates resources that all your Azure hosted clusters share, and recreates them if they're missing:

- Resource group `$USER-hyperctl` with the OIDC issuer, the External DNS zone and the workload identities of the hosted clusters
- Two service principals, one for `hypershift` and one for External DNS. ⚠️ The `hypershift` one has Contributor and User Access Administrator on the whole subscription. Their credentials are stored in `azure/` of the [data directory](configuration.md#directories), by default `~/.local/share/hyperctl/azure`.

`hyperctl destroy shared --platform azure` deletes all of them once no Azure cluster is left. It only knows the clusters of its own data directory and deletes the whole resource group, also resources hyperctl didn't create: check that no other hyperctl installation uses these resources, and don't point `azure.resourceGroup` at a group that holds anything else.

# Concepts

hyperctl creates clusters for Karpenter development and keeps track of them on your machine. This page explains the words the other docs use.

## Cluster model

```
platform aws or azure
└── standalone cluster          OpenShift (AWS) or AKS (Azure), its own control plane
    └── HyperShift operator     optional, makes it a management cluster
        └── hosted cluster(s)   AutoNode HostedClusters, Karpenter runs in their control plane
```

| Term | Meaning |
|---|---|
| Platform | The cloud: `aws` or `azure`. A hosted cluster has the platform of its management cluster. |
| Standalone cluster | A cluster with its own control plane: OpenShift installed with `openshift-install` on AWS, AKS on Azure. |
| Management cluster | A standalone cluster with the HyperShift operator installed. |
| Hosted cluster | A HyperShift HostedCluster with AutoNode (Karpenter) enabled. Its control plane runs as pods on the management cluster. |
| Setup | What `create` builds: `standalone` (cluster only), `management` (cluster and HyperShift operator) or `hosted` (all of it, plus a hosted cluster). |
| Shared resources | Cloud resources reused by all clusters of a platform, created on the first `create` that needs them and kept after `destroy`. See [AWS](aws.md#shared-resources) and [Azure](azure.md#shared-resources). |
| Current cluster | The cluster that `status`, `kubeconfig`, `image` and `destroy` act on when `--cluster` doesn't name one. Like a kubectl context. |

## Names

Every cluster has a name, unique across all clusters including hosted ones. Commands select clusters by these names, and the kubeconfig contexts are named after them.

- Standalone or management cluster: `--name`, default `$USER-karpenter`.
- Hosted cluster: `--hosted-name`, default the first free of `<name>-workload`, `<name>-workload-2`, ...

## Current cluster

- `create` makes the cluster it creates current; for setup `hosted` that's the management cluster.
- `hyperctl use <name>` makes any cluster current, also a hosted one.
- `hyperctl list` shows all clusters and marks the current one with `*`.
- Destroying the current cluster makes its management cluster current, or leaves no current cluster.

A command that needs one hosted cluster, like `image`, takes the only hosted cluster of the current management cluster. With several, name one with `--cluster`.

## Lifecycle

hyperctl records a cluster before it creates the first cloud resource and forgets it after the last one is deleted. A cluster whose creation stopped, e.g. on an error or Ctrl-C, stays recorded as incomplete: `status` shows it, and it has to be destroyed before a cluster with that name can be created again.

The records live in the data directory, see [Configuration](configuration.md#directories).

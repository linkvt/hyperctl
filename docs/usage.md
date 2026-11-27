# Usage

How to create, use and destroy clusters. Flags and their defaults: `hyperctl <command> --help`. The words used here are explained in [Concepts](concepts.md).

## Create

```sh
hyperctl create
```

In a terminal, `create` asks for every value you didn't pass as a flag, prefilled with the defaults, and shows a summary to confirm. With `--yes`, or without a terminal, it asks nothing: it uses the defaults and fails if a required value is missing.

### New cluster or add to one

| You pass | `create` does |
|---|---|
| `--name <new>` | Creates a new cluster. |
| `--cluster <existing>` | Adds to that cluster, or to the management cluster of a hosted cluster. |
| neither | Adds to the current cluster. In a terminal it first asks whether to add to it or create a new one. Without an installed current cluster, it creates a new one. |

`--name` and `--cluster` together are rejected.

### Setup

| `--setup` | New cluster | Added to an existing cluster |
|---|---|---|
| `hosted` | Standalone cluster, HyperShift operator, hosted cluster | Installs or updates the operator, creates a hosted cluster (default) |
| `management` | Standalone cluster, HyperShift operator | Only installs or updates the operator |
| `standalone` | Standalone cluster only | not allowed |

For new clusters, the default is the setup you used last. Adding to a cluster always (re)installs the HyperShift operator, which updates it to the `hypershift` binary you use. Flags the setup doesn't use, e.g. `--hosted-name` without a hosted cluster, are rejected.

### Release

A version of an official release (`5.1.0-ec.0`) or a release image pull spec, e.g. of a nightly. In a terminal you can browse the [release controller](https://amd64.ocp.releases.ci.openshift.org/); `hyperctl releases [stream]` lists the same from the command line.

Before creating anything, `create` checks that your pull secret can pull the release image. CI release images (`registry.ci.openshift.org`) need a login to the CI registry; `create` prints the commands to add it to your pull secret.

The release applies only to the clusters the run creates. A new hosted cluster takes the release of its management cluster unless you pass `--release`. An AKS cluster without HyperShift needs no release.

### Adding to an existing cluster

The existing cluster decides the platform and the values it recorded: region, base domain and pull secret. `create` doesn't ask for recorded values, and flags with other values are rejected. `--instance-type` and `--workers` only apply to new clusters.

<details>
<summary>Recording: add a hosted cluster to the current cluster</summary>

![hyperctl create adding a hosted cluster](assets/add.gif)

<sub>The run took 20 minutes; the recording is shortened to 40 seconds: waits are cut and long output of `hypershift` is skipped.</sub>

</details>

### Examples

```sh
# Everything from flags, ask nothing
hyperctl create --yes --platform aws --setup hosted --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# Management cluster without a hosted cluster
hyperctl create --setup management --name "$USER-mgmt" --release 5.1.0-ec.0

# Azure: AKS with HyperShift and a hosted cluster
hyperctl create --platform azure --setup hosted --release 5.1.0-ec.0 --name "$USER-aks"

# Azure: AKS only
hyperctl create --platform azure --setup standalone --name "$USER-aks"

# A second hosted cluster with another release on the current cluster
hyperctl create --release 5.2.0-ec.1

# A hosted cluster on another cluster
hyperctl create --cluster "$USER-second" --hosted-name "$USER-second-5-2" --release 5.2.0-ec.1

# Update the HyperShift operator of the current cluster to your hypershift binary
hyperctl create --setup management
```

Several `create` runs can run at the same time for different clusters.

## Work with clusters

```sh
hyperctl list                # all clusters, * marks the current one
hyperctl use <name>          # make a cluster current
hyperctl status              # state of the current cluster and its relatives
```

`status` shows the management cluster with its hosted clusters, or a hosted cluster with its management cluster: whether the API responds, whether the HyperShift operator is installed, whether the hosted clusters are available, their image overrides, and how to proceed with incomplete clusters.

<details>
<summary>Recording: list, switch and use clusters on AWS and Azure</summary>

![hyperctl list, use, status and kubeconfig](assets/clusters.gif)

<sub>Waits shortened.</sub>

</details>

### Kubeconfig

```sh
export KUBECONFIG=$(hyperctl kubeconfig)
oc get nodes                                          # the hosted cluster
oc --context "$USER-karpenter" get hostedclusters -A  # the management cluster
```

`kubeconfig` prints kubeconfig paths separated by `:`, ready for `KUBECONFIG`:

| Selected cluster | Printed |
|---|---|
| Hosted cluster | Hosted, then its management cluster. The hosted cluster is the current context. |
| Management cluster with one hosted cluster | Same as selecting that hosted cluster |
| Management cluster with several hosted clusters | Only the management cluster; the hosted clusters are listed on stderr, pick one with `--cluster` |
| Standalone cluster | The cluster |

Each context is named after its cluster. The hosted kubeconfig is available while the hosted cluster is still installing; before that, only the management cluster is printed, or `kubeconfig --cluster <hosted>` fails.

### Image overrides

Replace a Karpenter image of a hosted cluster with your own build:

```sh
hyperctl image karpenter-operator quay.io/<you>/karpenter-operator:<unique-tag>
hyperctl image karpenter-provider quay.io/<you>/karpenter-provider:<unique-tag>
hyperctl image karpenter-provider --reset   # back to the release image
```

| Component | Replaces |
|---|---|
| `karpenter-operator` | The karpenter-operator that deploys Karpenter |
| `karpenter-provider` | Karpenter itself, the provider of the platform (AWS or Azure) |

- The deployment rolls out with the new image; `status` lists the overrides.
- The cluster must be able to pull the image, e.g. from a public repository.
- Setting the same image again only restarts the pods, and nodes may keep the cached image. Push every build with a unique tag.
- Overrides need the standalone karpenter-operator, the default. With `create --standalone-karpenter-operator=false` (AWS only) they have no effect.

## Destroy

```sh
hyperctl destroy                     # the current cluster
hyperctl destroy --cluster <name>    # another one
```

- Destroying a hosted cluster keeps its management cluster.
- Destroying a management cluster destroys its hosted clusters first.
- It asks for confirmation; without a terminal it requires `--yes`.
- If the management cluster is unreachable, the cloud resources of its hosted clusters are still deleted.

<details>
<summary>Recording: destroy an AKS management cluster with its hosted cluster</summary>

![hyperctl destroy](assets/destroy.gif)

<sub>The run took 18 minutes; the recording is shortened to 18 seconds: waits are cut and long output of `hypershift` is skipped.</sub>

</details>

[Shared resources](concepts.md) are kept. Delete them once no cluster of the platform is left:

```sh
hyperctl destroy shared --platform aws    # or azure
```

What that deletes: [AWS](aws.md#shared-resources), [Azure](azure.md#shared-resources).

## Cache

`create` extracts `openshift-install` once per release and caches it. `hyperctl cache clean` removes the cached binaries.

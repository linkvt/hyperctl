# hyperctl

Interactive CLI that creates a HyperShift management cluster on AWS or Azure for Karpenter development, optionally with an AutoNode hosted cluster, or a cluster without HyperShift.
On AWS the cluster is OpenShift, installed with `openshift-install`; on Azure it is AKS.
hyperctl tracks the clusters it creates and keeps a current cluster, like a kubectl context, so `status`, `kubeconfig`, `image` and `destroy` need no cluster names.

## Prerequisites

- Go 1.27.1 or later
- `oc`
- `hypershift` built from [openshift/hypershift](https://github.com/openshift/hypershift) (`make build`), in `PATH` or configured via `hypershift` in the config file, not needed for setup `standalone`
- A pull secret, by default at `~/.openshift/pull-secret-latest.json`, not needed for an AKS cluster without HyperShift

For AWS:

- Credentials of an IAM user in the `[default]` profile of `~/.aws/credentials`, not temporary SAML credentials.
  The HyperShift operator uses this file to write OIDC documents to S3.
- A public Route53 zone for the base domain, by default `autoscaling-dev.devcluster.openshift.com`

For Azure:

- The Azure CLI `az`, logged in with `az login` to the subscription to use; for HyperShift with permission to create applications and role assignments (e.g. Owner)
- For HyperShift, an Azure DNS zone for the base domain, by default `azure.autoscaling-dev.devcluster.openshift.com` in resource group `os4-common`

## Installation

```sh
go install github.com/linkvt/hyperctl@latest
```

Or from a local checkout:

```sh
go install .
```

Both install into `$(go env GOPATH)/bin`, which needs to be in `PATH`.

## Quick start

```sh
# Prompts for missing values, prefilled with defaults
hyperctl create

# Use both clusters, the hosted cluster is the current context
export KUBECONFIG=$(hyperctl kubeconfig)
oc get nodes

# Check the clusters
hyperctl status

# Destroy the hosted and the management cluster
hyperctl destroy
```

## Commands

| Command | Purpose |
|---|---|
| `hyperctl create` | Create a cluster, optionally with the HyperShift operator and an AutoNode hosted cluster, skipping clusters that are installed |
| `hyperctl list` | List the clusters, marking the current one with `*` |
| `hyperctl use <name>` | Make a cluster the current cluster |
| `hyperctl status` | Show the cluster and its management or hosted clusters, whether the management cluster responds and the hosted clusters are available |
| `hyperctl kubeconfig` | Print the kubeconfig paths for `KUBECONFIG`: of a hosted cluster and its management cluster, or of a cluster without hosted cluster |
| `hyperctl destroy` | Destroy the cluster, a management cluster after its hosted clusters |
| `hyperctl destroy shared --platform aws\|azure` | Destroy the resources that the clusters of a platform share |
| `hyperctl image karpenter-operator\|karpenter-provider <image>` | Override the Karpenter operator or Karpenter provider image of the hosted cluster (`--reset` to remove) |
| `hyperctl releases [stream]` | List release streams or the accepted releases of a stream (`--all` for all) |
| `hyperctl cache clean` | Remove cached installer binaries |

`status`, `kubeconfig`, `image` and `destroy` act on the current cluster, or on the cluster named by `--cluster <name>`.
`hyperctl <command> --help` lists all flags and defaults.

## Creating clusters

```sh
# Values from flags, create asks only for the others (--yes takes their defaults)
hyperctl create --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# Management cluster and HyperShift operator, no hosted cluster
hyperctl create --setup management --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# On Azure
hyperctl create --platform azure --release 5.1.0-ec.0 --name "$USER-aks"

# AKS cluster without HyperShift
hyperctl create --platform azure --setup standalone --name "$USER-aks"

# A second management cluster next to the current one
hyperctl create --name "$USER-second" --release 5.1.0-ec.0

# A further hosted cluster with another release on the current cluster
hyperctl create --release 5.2.0-ec.1

# A hosted cluster on another existing cluster, installing the HyperShift operator if missing
hyperctl create --cluster "$USER-second" --hosted-name "$USER-second-5-2" --release 5.2.0-ec.1

# Only update the HyperShift operator of the current cluster to the hypershift binary in use
hyperctl create --setup management
```

In a terminal, `create` asks for every value not given as a flag, prefilled with the defaults, and asks to confirm a summary.
With `--yes`, or without a terminal, it asks nothing: it takes the defaults and fails if a required value is missing.

- **New or add:** `create` either creates a new cluster or adds to an existing one. `--name` creates a new cluster, `--cluster` adds to an existing one, or to the management cluster of a hosted cluster. Without either, `create` adds to the current cluster; in a terminal it asks first whether to add to the current cluster or create a new one. Without a current cluster, or if its installation did not complete, `create` creates a new one.
- **Names:** `--name` names a new standalone or management cluster, by default `$USER-karpenter`. `--hosted-name` names a new hosted cluster, by default the first free of `<name>-workload`, `<name>-workload-2`, and so on. Names are unique across all clusters, a name of an existing cluster is rejected. The kubeconfig contexts are named after the clusters.
- **Platform:** `create` asks for the platform of a new cluster, or uses `--platform` (default `aws`). An existing cluster determines the platform and `create` reports it.
- **Setup:** for a new cluster, `--setup hosted` creates a management cluster with an AutoNode hosted cluster, `management` the management cluster and HyperShift operator, `standalone` only the cluster, without anything HyperShift needs. `create` remembers the last choice for new clusters. Added to an existing cluster, `hosted` (the default) installs or updates the HyperShift operator and creates a hosted cluster, `management` only installs or updates the operator. Flags the setup doesn't use, like `--hosted-name` without a hosted cluster or `--release` for a standalone AKS cluster, are rejected.
- **Release:** not needed for an AKS cluster without HyperShift. A version of an official release (`5.1.0-ec.0`) or a release image pull spec, e.g. of a nightly. Interactively, `create` lets you browse the [release controller](https://amd64.ocp.releases.ci.openshift.org/). `create` checks that the pull secret can pull the release image before creating anything; CI release images need a login to the CI registry, and `create` explains how to add it to the pull secret.
- **Size:** the size prompt offers presets for the cluster, see the platforms below. `--instance-type` and `--workers` skip it.
- **Region:** `--region` is the AWS region or the Azure location. It defaults to the config file, then the last used region of the platform, then the region of the AWS profile or `westus3`.

Adding to an existing cluster installs the HyperShift operator on every run, which updates it to the hypershift binary in use.
An existing cluster determines the region, base domain and pull secret of its hosted clusters, `create` doesn't ask for them, and flags that differ are rejected. The release applies only to the clusters `create` creates: a new hosted cluster takes the release of its management cluster unless `--release` says otherwise, and `--release` without a cluster to create is rejected.
Several `create` runs may run at the same time for different clusters.
A cluster whose creation did not complete has to be destroyed first; if only the hosted cluster failed, `hyperctl destroy --cluster <hosted cluster>` keeps the management cluster.

### AWS

1. Check the AWS account: IAM user credentials, DNS leftovers of previous clusters with the same names, and for HyperShift the EC2 Spot service-linked role and the OIDC bucket (created and made public-readable if missing)
2. Install the management cluster with `openshift-install` (about 45 minutes). The default is openshift-install's own: 3 control plane and 3 worker `m6i.xlarge` nodes across the zones of the region. The size prompt also offers compact clusters with 1 worker in one zone; `--workers=0` installs a compact cluster without a worker.
3. Unless setup `standalone`, install the HyperShift operator with the standalone karpenter-operator, or with `--standalone-karpenter-operator=false` the one embedded in the HyperShift operator image
4. For setup `hosted`, create the AutoNode hosted cluster and write its kubeconfig

Clusters whose instances were pruned leave Route53 records behind; `create` and `destroy` list the records of clusters with the same names and delete them after confirmation (or with `--yes`).

### Azure

1. Check the Azure login (`az account show`) and that the location offers the node VM size without restrictions; see Region above for the default location
2. Unless setup `standalone`, create or reuse the shared resources, see below
3. Create an AKS cluster in 2 zones in resource group `<name>-rg` (about 10 minutes). The size prompt offers 3 nodes `Standard_D4s_v5` and the size from the config file.
4. Unless setup `standalone`, install the Prometheus and Route CRDs and the HyperShift operator with External DNS and the standalone karpenter-operator, which Azure AutoNode requires
5. For setup `hosted`, create the workload identities and infrastructure with hypershift and the hosted cluster with 2 workers

Shared resources, created when missing on every `create` and kept when destroying clusters:

| Resource | Default name |
|---|---|
| Resource group with the OIDC issuer, External DNS zone and workload identities | `$USER-hyperctl` |
| OIDC issuer, a storage account with public blobs, and its signing key in the data directory | `$USER` in lowercase letters and digits + `hyperctloidc`, at most 24 characters |
| External DNS zone, delegated by an NS record in the base domain zone | `$USER-hyperctl.<base domain>` |
| Service principal of the hypershift CLI with Contributor and User Access Administrator on the subscription | `$USER-hyperctl` |
| Service principal of External DNS with Reader and Contributor on its zone | `$USER-hyperctl-external-dns` |

The service principal credentials and the signing key are kept in `~/.local/share/hyperctl/azure`.

## Destroying clusters

`hyperctl destroy` destroys the current cluster, `--cluster <name>` another one. A management cluster is destroyed after its hosted clusters, a hosted cluster alone keeps its management cluster.
If the current cluster is destroyed, its management cluster becomes current, or there is no current cluster.
It asks for confirmation; without a terminal it requires `--yes`.

On AWS, `destroy` deletes the hosted cluster with hypershift and the management cluster with `openshift-install`, then DNS leftovers of both.
On Azure, it deletes the hosted cluster with hypershift, together with its DNS records and workload identities, and then the resource group of the AKS cluster.
If the management cluster is unreachable, the cloud resources of the hosted cluster are still deleted.

`destroy shared --platform azure` deletes the shared Azure resources once no Azure cluster is left.
`destroy shared --platform aws` deletes nothing: the OIDC bucket is kept, and the EC2 Spot service-linked role belongs to the whole account.

## Working with clusters

### Current cluster

`create` makes the cluster it creates the current one, the management cluster for setup `hosted`.
`hyperctl list` shows all clusters with their hosted clusters, and `hyperctl use <name>` selects another one, also a hosted cluster.
A hosted cluster is reached through its management cluster: `image` acts on the only hosted cluster of the current management cluster, and with several on the one named by `--cluster`.

### Kubeconfig

```sh
# Both clusters, the hosted cluster is the current context
export KUBECONFIG=$(hyperctl kubeconfig)
oc --context "$USER-karpenter" get hostedclusters -n clusters

# A hosted cluster of a management cluster with several
export KUBECONFIG=$(hyperctl kubeconfig --cluster "$USER-karpenter-workload")
```

`kubeconfig` names each context after its cluster, so that both kubeconfigs work together in one `KUBECONFIG`.
A management cluster with one hosted cluster prints both kubeconfigs, one with several only its own and lists the hosted clusters on stderr.
The hosted kubeconfig becomes available while the hosted cluster is still installing; until then, `kubeconfig` returns only the management cluster's, or fails for a hosted cluster named by `--cluster`.

### Status

`status` shows the cluster with its hosted clusters, or a hosted cluster with its management cluster: whether the management cluster responds and has the HyperShift operator, whether the hosted clusters are available and the image overrides.
It detects an unreachable management cluster, e.g. after its instances were pruned.

### Image overrides

`image` sets an override that the standalone karpenter-operator picks up and rolls out, on both platforms: `karpenter-operator` replaces the operator, `karpenter-provider` the Karpenter provider of the platform.
Overrides have no effect with `--standalone-karpenter-operator=false`.
An unchanged image only restarts the pods, which may keep a cached image; push images with unique tags.

## Configuration

Flags override the config file, which overrides the last used values and the built-in defaults.
The SSH key is asked once and saved; `sshKey: ""` disables it.

| Key | Sets |
|---|---|
| `pullSecret` | Pull secret file |
| `sshKey` | Path of the SSH public key for node access |
| `hypershift` | Path of the hypershift binary, saved when `create` asks for it |
| `region`, `baseDomain` | AWS region and base domain |
| `oidcBucket` | S3 bucket for the OIDC documents on AWS |
| `mgmtInstanceType`, `mgmtWorkers` | Size of the AWS management cluster |
| `azure.location`, `azure.baseDomain` | Azure location and base domain |
| `azure.baseDomainResourceGroup` | Resource group of the base domain's DNS zone |
| `azure.resourceGroup`, `azure.oidcStorageAccount`, `azure.dnsZone` | Names of the shared Azure resources |
| `azure.mgmtVMSize`, `azure.mgmtNodes` | Size of the AKS management cluster |

hyperctl follows the XDG Base Directory Specification on macOS and Linux:

| Directory | Default | Content |
|---|---|---|
| Config | `~/.config/hyperctl/config.yaml` | Your values instead of the built-in defaults, see the keys above |
| Data | `~/.local/share/hyperctl` | Cluster records, installer directories, kubeconfigs, Azure credentials and signing key |
| State | `~/.local/state/hyperctl` | Names of the tracked clusters, last used release, setup and regions |
| Cache | `~/.cache/hyperctl` | `openshift-install` binaries per release |

## Development

```sh
make verify   # formats, lints and tests; needs golangci-lint
```

# hyperctl

Interactive CLI that creates a HyperShift management cluster on AWS or Azure for Karpenter development, optionally with an AutoNode hosted cluster, or a cluster without HyperShift.
On AWS the cluster is OpenShift, installed with `openshift-install`; on Azure it is AKS.
hyperctl tracks the clusters it creates, so `status`, `kubeconfig` and `destroy` need no cluster names.

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
| `hyperctl status` | Show the clusters, whether the management cluster responds and the hosted cluster is available |
| `hyperctl kubeconfig [hosted\|mgmt]` | Print the kubeconfig paths of both clusters for `KUBECONFIG`, or of one |
| `hyperctl destroy [all\|hosted]` | Destroy the hosted and, by default, the management cluster |
| `hyperctl destroy shared --platform aws\|azure` | Destroy the resources that the clusters of a platform share |
| `hyperctl image karpenter-operator\|karpenter-provider <image>` | Override the Karpenter operator or Karpenter provider image of the hosted cluster (`--reset` to remove) |
| `hyperctl releases [stream]` | List release streams or the accepted releases of a stream (`--all` for all) |
| `hyperctl cache clean` | Remove cached installer binaries |

`hyperctl <command> --help` lists all flags and defaults.

## Creating clusters

```sh
# Values from flags, create asks only for the others (--yes takes their defaults)
hyperctl create --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# Management cluster and HyperShift operator, no hosted cluster
hyperctl create --setup management --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# On Azure
hyperctl create --platform azure --release 5.1.0-ec.0

# AKS cluster without HyperShift
hyperctl create --platform azure --setup standalone
```

In a terminal, `create` asks for every value not given as a flag, prefilled with the defaults, and asks to confirm a summary.
With `--yes`, or without a terminal, it asks nothing: it takes the defaults and fails if a required value is missing.

- **Platform:** an existing management cluster determines the platform and `create` reports it; otherwise `create` asks for it, or uses `--platform` (default `aws`).
- **Setup:** `--setup hosted` creates a management cluster with an AutoNode hosted cluster, `management` the management cluster and HyperShift operator, `standalone` only the cluster, without anything HyperShift needs. `create` remembers the last choice. Flags the setup doesn't use, like `--hosted-name` without a hosted cluster or `--release` for a standalone AKS cluster, are rejected.
- **Release:** not needed for an AKS cluster without HyperShift. A version of an official release (`5.1.0-ec.0`) or a release image pull spec, e.g. of a nightly. Interactively, `create` lets you browse the [release controller](https://amd64.ocp.releases.ci.openshift.org/). `create` checks that the pull secret can pull the release image before creating anything; CI release images need a login to the CI registry, and `create` explains how to add it to the pull secret.
- **Size:** the size prompt offers presets for the cluster, see the platforms below. `--instance-type` and `--workers` skip it.
- **Names:** the management cluster is `$USER-karpenter` and the hosted cluster `<name>-workload` unless `--name` and `--hosted-name` say otherwise. The kubeconfig contexts are named after them.
- **Region:** `--region` is the AWS region or the Azure location. It defaults to the config file, then the last used region of the platform, then the region of the AWS profile or `westus3`.

hyperctl tracks one cluster, with at most one hosted cluster; to create others, destroy them first.
Re-running `create` skips installed clusters and creates the missing ones; with another setup it adds the HyperShift operator and the hosted cluster to an existing cluster. It installs the operator on every run, which updates it to the hypershift binary in use.
An existing management cluster also determines the release, region, base domain and pull secret of its hosted cluster.
A cluster whose creation did not complete has to be destroyed first; if only the hosted cluster failed, `hyperctl destroy hosted` keeps the management cluster.

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

`hyperctl destroy` destroys the hosted cluster and then the management cluster, `destroy hosted` only the hosted cluster.
It asks for confirmation; without a terminal it requires `--yes`.

On AWS, `destroy` deletes the hosted cluster with hypershift and the management cluster with `openshift-install`, then DNS leftovers of both.
On Azure, it deletes the hosted cluster with hypershift, together with its DNS records and workload identities, and then the resource group of the AKS cluster.
If the management cluster is unreachable, the cloud resources of the hosted cluster are still deleted.

`destroy shared --platform azure` deletes the shared Azure resources once no Azure management cluster is left.
`destroy shared --platform aws` deletes nothing: the OIDC bucket is kept, and the EC2 Spot service-linked role belongs to the whole account.

## Working with clusters

### Kubeconfig

```sh
# Both clusters, the hosted cluster is the current context
export KUBECONFIG=$(hyperctl kubeconfig)
oc --context "$USER-karpenter" get hostedclusters -n clusters

# Only one of them
export KUBECONFIG=$(hyperctl kubeconfig mgmt)
```

`kubeconfig` names each context after its cluster, so that both kubeconfigs work together in one `KUBECONFIG`.
The hosted kubeconfig becomes available while the hosted cluster is still installing; until then, `kubeconfig` returns only the management cluster's.

### Status

`status` shows the tracked clusters, whether the management cluster responds and has the HyperShift operator, whether the hosted cluster is available and the image overrides.
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

# hyperctl

Interactive CLI to create a HyperShift management cluster on AWS or Azure for Karpenter development, optionally with an AutoNode-enabled hosted cluster.

## Prerequisites

- Go 1.27.1 or later
- `oc`
- `hypershift` built from [openshift/hypershift](https://github.com/openshift/hypershift) (`make build`), in `PATH` or configured via `hypershift` in the config file
- AWS credentials of an IAM user in the `[default]` profile of `~/.aws/credentials` (not temporary SAML credentials).
  The HyperShift operator uses this file to write OIDC documents to S3.
- A pull secret, by default at `~/.openshift/pull-secret-latest.json`
- For Azure: the Azure CLI `az`, logged in with `az login` to the subscription to use, with permission to create applications and role assignments (e.g. Owner)

## Installation

```sh
go install github.com/linkvt/hyperctl@latest
```

Or from a local checkout:

```sh
go install .
```

Both install into `$(go env GOPATH)/bin`, which needs to be in `PATH`.

## Usage

```sh
# Prompts for missing values, prefilled with defaults
hyperctl create

# Non-interactive, including the hosted cluster
hyperctl create --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# Management cluster and HyperShift operator only
hyperctl create --management-only --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# Both clusters, the hosted cluster is the current context
export KUBECONFIG=$(hyperctl kubeconfig)
oc get nodes
oc --context "$USER-karpenter" get hostedclusters -n clusters

# Only one of them
export KUBECONFIG=$(hyperctl kubeconfig mgmt)
```

`kubeconfig` names the context, cluster and user of each kubeconfig after the cluster (the user gets an `-admin` suffix), so that both can be combined.
If the hosted kubeconfig is not saved locally, it fetches and caches it from the management cluster as soon as the admin kubeconfig secret exists, even during installation.
Until then, the default command returns only the management kubeconfig; `kubeconfig hosted` reports that it is not available yet.
Cached kubeconfigs work without contacting the management cluster.
`kubeconfig mgmt` never queries it.
Kubeconfig availability does not mean the hosted cluster is Ready.

`create` installs a management cluster and the HyperShift operator.
Interactive setup remembers the previous choice between management-only and hosted-cluster modes.
Use `--management-only` or `--management-only=false` to select a mode non-interactively.
Hosted-cluster mode creates an AutoNode-enabled cluster.
Re-running `create` skips an installed management cluster.
If hosted-cluster creation fails, clean up only that cluster with `hyperctl destroy hosted` before retrying; the management cluster remains installed.

1. Check the AWS account: IAM user credentials, EC2 Spot service-linked role, OIDC bucket (created and made public-readable if missing), DNS leftovers
2. Install the management cluster with `openshift-install` (about 45 minutes), by default as 3 control-plane and 1 worker `t3a.xlarge` nodes in one zone
3. Install the HyperShift operator, by default with the standalone karpenter-operator (`--enable-standalone-karpenter-operator`).
   Use `--standalone-karpenter-operator=false` for the one embedded in the HyperShift operator image.
   The `hyperctl image karpenter-operator` override only applies to the standalone one.
   Re-running `create` with a different value switches the mode of the installed operator.
4. Unless management-only mode is selected, create the AutoNode hosted cluster and write its kubeconfig

`hyperctl create --help` lists all flags and defaults.
Set `--workers=0` to install a three-node compact management cluster without a worker.

| Command | Purpose |
|---|---|
| `hyperctl create` | Create the management cluster and optionally its hosted cluster, skipping an installed management cluster |
| `hyperctl status` | Show the clusters, whether the management cluster responds and the hosted cluster is available |
| `hyperctl kubeconfig [hosted\|mgmt]` | Print the kubeconfig paths of both clusters for `KUBECONFIG`, or of one |
| `hyperctl destroy [all\|hosted\|shared]` | Destroy the hosted and, by default, the management cluster, or the shared Azure resources |
| `hyperctl image karpenter-operator\|karpenter-provider <image>` | Override the Karpenter operator or Karpenter provider image of the hosted cluster (`--reset` to remove) |
| `hyperctl releases [stream]` | List release streams or the accepted releases of a stream (`--all` for all) |
| `hyperctl cache clean` | Remove cached installer binaries |

Releases are given as a version of an official release (`5.1.0-ec.0`) or as a release image pull spec, e.g. of a nightly.
Interactively, `create` lets you browse the [release controller](https://amd64.ocp.releases.ci.openshift.org/).

`image karpenter-operator` sets the `hypershift.openshift.io/karpenter-operator-image` annotation of the hosted cluster, `image karpenter-provider` sets the `autoscaling.openshift.io/karpenter-image` annotation of its HostedControlPlane, which the standalone karpenter-operator reads on both clouds. Either rolls out the deployment.
Both have no effect with `--standalone-karpenter-operator=false`.
Setting an unchanged image restarts the pods instead, which may keep a cached image; push images with unique tags.

Instances of stopped clusters get pruned, which leaves broken clusters and Route53 records behind.
`status` detects an unreachable management cluster, `create` and `destroy` list DNS leftovers of previous clusters with the same names and delete them after confirmation (or with `--yes`).

## Azure

`create --platform azure` creates an AKS management cluster and an Azure hosted cluster with AutoNode, following a verified manual setup.
An existing management cluster determines the platform and `create` reports it; otherwise `create` asks for it.

1. Check the Azure login (`az account show`) and that the location offers the node VM size, by default `Standard_D4s_v5`, without restrictions; the location defaults to the last used one, or `westus3`
2. Create or reuse the resources that all clusters share, see below
3. Create an AKS cluster with 3 nodes in 2 zones in resource group `<name>-rg` (about 10 minutes)
4. Install the Prometheus and Route CRDs and the HyperShift operator with External DNS and the standalone karpenter-operator, which Azure AutoNode requires
5. Create the workload identities (`hypershift create iam azure --enable-karpenter`) and infrastructure (`hypershift create infra azure`), assign the roles of the workload identities, and create the hosted cluster with 2 workers

hyperctl assigns the roles of the workload identities itself, as `hypershift create infra azure --assign-identity-roles` reads Microsoft Graph with the service principal, which it usually may not.
The hosted cluster gets the `hypershift.openshift.io/pod-security-admission-label-override=baseline` annotation, as AKS enforces the restricted pod security level.

Shared resources, created when missing on every `create` and kept when destroying clusters:

| Resource | Default name |
|---|---|
| Resource group with the OIDC issuer, External DNS zone and workload identities | `$USER-hyperctl` |
| OIDC issuer, a storage account with public blobs, and its signing key in the data directory | `${USER}hyperctloidc` |
| External DNS zone, delegated by an NS record in the base domain zone | `$USER-hyperctl.<base domain>` |
| Service principal of the hypershift CLI with Contributor and User Access Administrator on the subscription | `$USER-hyperctl` |
| Service principal of External DNS with Reader and Contributor on its zone | `$USER-hyperctl-external-dns` |

The service principal credentials and the signing key are kept in `~/.local/share/hyperctl/azure`.
`hyperctl destroy shared` deletes all of them once no management cluster is left.

`destroy` runs `hypershift destroy cluster azure`, which deletes the HostedCluster and the resource groups of the hosted cluster (`hypershift destroy infra azure` if the management cluster is unreachable). hyperctl then deletes the External DNS records, the `*.apps` record of the hosted cluster in the base domain zone (only if owned by its infra ID) and the role assignment of the ingress identity on that zone, runs `hypershift destroy iam azure` for the workload identities and deletes the resource group of the AKS cluster.

On Azure `hyperctl image karpenter-operator` follows `hypershift.openshift.io/karpenter-operator-image`, and `hyperctl image karpenter-provider` overrides the Azure provider that the standalone karpenter-operator deploys.

## Files

hyperctl follows the XDG Base Directory Specification on macOS and Linux:

| Directory | Default | Content |
|---|---|---|
| Config | `~/.config/hyperctl/config.yaml` | User defaults, see below |
| Data | `~/.local/share/hyperctl` | Installer directories, hosted cluster kubeconfigs, needed to destroy clusters |
| State | `~/.local/state/hyperctl` | Current cluster, last used release and regions |
| Cache | `~/.cache/hyperctl` | `openshift-install` binaries per release |

Config values override built-in defaults, flags override config values.
Without a region in the config, `create` uses the last used region of the platform.
The SSH key is asked once and saved; `sshKey: ""` disables it.

```yaml
region: us-east-1
baseDomain: autoscaling-dev.devcluster.openshift.com
pullSecret: ~/.openshift/pull-secret-latest.json
sshKey: ~/.ssh/id_ed25519.pub
oidcBucket: myuser-hypershift-oidc
# Management cluster size, mgmtWorkers: 0 runs a compact cluster in one zone
mgmtInstanceType: t3a.xlarge
mgmtWorkers: 0
hypershift: ~/repos/openshift/hypershift/bin/hypershift
# Azure clusters
azure:
  location: germanywestcentral
  baseDomain: azure.autoscaling-dev.devcluster.openshift.com
  baseDomainResourceGroup: os4-common
  resourceGroup: myuser-hyperctl
  oidcStorageAccount: myuserhyperctloidc
  dnsZone: myuser-hyperctl.azure.autoscaling-dev.devcluster.openshift.com
  mgmtVMSize: Standard_D4s_v5
  mgmtNodes: 3
```

## Development

```sh
go test ./...
golangci-lint run ./...   # includes the modernize linter, see .golangci.yml
golangci-lint fmt ./...
```

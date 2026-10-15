# hyperctl

Interactive CLI to create a HyperShift management cluster on AWS for
Karpenter development, optionally with an AutoNode-enabled hosted cluster.

## Prerequisites

- Go 1.27.1 or later
- `oc`
- `hypershift` built from [openshift/hypershift](https://github.com/openshift/hypershift)
  (`make build`), in `PATH` or configured via `hypershift` in the config file
- AWS credentials of an IAM user in the `[default]` profile of
  `~/.aws/credentials` (not temporary SAML credentials). The HyperShift
  operator uses this file to write OIDC documents to S3.
- A pull secret, by default at `~/.openshift/pull-secret-latest.json`

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

`kubeconfig` names the context, cluster and user of each kubeconfig after the
cluster (the user gets an `-admin` suffix), so that both can be combined.

`create` installs a management cluster and the HyperShift operator. Interactive
setup remembers the previous choice between management-only and hosted-cluster
modes. Use `--management-only` or `--management-only=false` to select a mode
non-interactively. Hosted-cluster mode creates an AutoNode-enabled cluster.
Re-running `create` skips what already exists:

1. Check the AWS account: IAM user credentials, EC2 Spot service-linked role,
   OIDC bucket (created and made public-readable if missing), DNS leftovers
2. Install the management cluster with `openshift-install` (about 45 minutes),
   by default as a compact cluster of 3 `t3a.xlarge` nodes in one zone
3. Install the HyperShift operator, by default with the standalone
   karpenter-operator (`--enable-standalone-karpenter-operator`). Use
   `--standalone-karpenter-operator=false` for the one embedded in the
   HyperShift operator image. The `hyperctl image karpenter-operator` override
   only applies to the standalone one. Re-running `create` with a different
   value switches the mode of the installed operator.
4. Unless management-only mode is selected, create the AutoNode hosted cluster
   and write its kubeconfig

`hyperctl create --help` lists all flags and defaults.

| Command | Purpose |
|---|---|
| `hyperctl create` | Create the management cluster and optionally its hosted cluster, resuming where a previous run stopped |
| `hyperctl status` | Show the clusters, whether the management cluster responds and the hosted cluster is available |
| `hyperctl kubeconfig [hosted\|mgmt]` | Print the kubeconfig paths of both clusters for `KUBECONFIG`, or of one |
| `hyperctl destroy [all\|hosted]` | Destroy the hosted and, by default, the management cluster |
| `hyperctl image karpenter-operator\|karpenter <image>` | Override the Karpenter operator or Karpenter image of the hosted cluster (`--reset` to remove) |
| `hyperctl releases [stream]` | List release streams or the accepted releases of a stream (`--all` for all) |
| `hyperctl cache clean` | Remove cached installer binaries |

Releases are given as a version of an official release (`5.1.0-ec.0`) or as a
release image pull spec, e.g. of a nightly. Interactively, `create` lets you
browse the [release controller](https://amd64.ocp.releases.ci.openshift.org/).

`image` sets the `hypershift.openshift.io/karpenter-operator-image` or
`hypershift.openshift.io/karpenter-provider-aws-image` annotation of the
hosted cluster, which rolls out the deployment. Setting an unchanged image
restarts the pods instead; hypershift only pulls `:latest` images again, other
tags may be served from the node's image cache.

Instances of stopped clusters get pruned, which leaves broken clusters and
Route53 records behind. `status` detects an unreachable management cluster,
`create` and `destroy` list DNS leftovers of previous clusters with the same
names and delete them after confirmation (or with `--yes`).

## Files

hyperctl follows the XDG Base Directory Specification on macOS and Linux:

| Directory | Default | Content |
|---|---|---|
| Config | `~/.config/hyperctl/config.yaml` | User defaults, see below |
| Data | `~/.local/share/hyperctl` | Installer directories, hosted cluster kubeconfigs, needed to destroy clusters |
| State | `~/.local/state/hyperctl` | Current cluster, last used release |
| Cache | `~/.cache/hyperctl` | `openshift-install` binaries per release |

Config values override built-in defaults, flags override config values. The
SSH key is asked once and saved; `sshKey: ""` disables it.

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
```

## Development

```sh
go test ./...
golangci-lint run ./...   # includes the modernize linter, see .golangci.yml
golangci-lint fmt ./...
```

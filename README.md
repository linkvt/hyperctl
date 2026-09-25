# hyperctl

Interactive CLI to create a HyperShift management cluster and an
AutoNode-enabled hosted cluster on AWS for Karpenter development.

## Prerequisites

- Go
- `oc`
- `hypershift` built from [openshift/hypershift](https://github.com/openshift/hypershift)
  (`make build`), in `PATH` or configured via `hypershift` in the config file
- AWS credentials of an IAM user in the `[default]` profile of
  `~/.aws/credentials` (not temporary SAML credentials). The HyperShift
  operator uses this file to write OIDC documents to S3.
- A pull secret, by default at `~/.openshift/pull-secret-latest.json`

## Usage

```sh
go install github.com/linkvt/hyperctl@latest

# Prompts for missing values, prefilled with defaults
hyperctl create

# Non-interactive
hyperctl create --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

# Hosted cluster
export KUBECONFIG=$(hyperctl kubeconfig)
# Management cluster
export KUBECONFIG=$(hyperctl kubeconfig mgmt)
```

`create` walks from any state to a management cluster with the HyperShift
operator and a hosted cluster with AutoNode enabled. Re-running it skips what
already exists:

1. Check the AWS account: IAM user credentials, EC2 Spot service-linked role,
   OIDC bucket (created and made public-readable if missing), DNS leftovers
2. Install the management cluster with `openshift-install` (about 45 minutes)
3. Install the HyperShift operator
4. Create the hosted cluster with AutoNode and write its kubeconfig

`hyperctl create --help` lists all flags and defaults.

| Command | Purpose |
|---|---|
| `hyperctl create` | Create the management and hosted cluster, resuming where a previous run stopped |
| `hyperctl status` | Show the clusters, whether the management cluster responds and the hosted cluster is available |
| `hyperctl kubeconfig [hosted\|mgmt]` | Print the kubeconfig path, hosted by default |
| `hyperctl destroy [all\|hosted]` | Destroy the hosted and, by default, the management cluster |
| `hyperctl releases [stream]` | List release streams or the accepted releases of a stream (`--all` for all) |
| `hyperctl cache clean` | Remove cached installer binaries |

Releases are given as a version of an official release (`5.1.0-ec.0`) or as a
release image pull spec, e.g. of a nightly. Interactively, `create` lets you
browse the [release controller](https://amd64.ocp.releases.ci.openshift.org/).

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
hypershift: ~/repos/openshift/hypershift/bin/hypershift
```

## Development

```sh
go test ./...
golangci-lint run ./...
```

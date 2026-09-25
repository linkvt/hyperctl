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
   OIDC bucket (created and made public-readable if missing)
2. Install the management cluster with `openshift-install` (about 45 minutes)
3. Install the HyperShift operator
4. Create the hosted cluster with AutoNode and write its kubeconfig

`hyperctl create --help` lists all flags and defaults.

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

# hyperctl

Interactive CLI to create a HyperShift management cluster and an
AutoNode-enabled hosted cluster on AWS for Karpenter development.

> Work in progress: `create` currently installs the management cluster only.

## Prerequisites

- Go
- `oc`
- AWS credentials of an IAM user in the default AWS profile (not temporary
  SAML credentials)
- A pull secret, by default at `~/.openshift/pull-secret-latest.json`

## Usage

```sh
go install github.com/linkvt/hyperctl@latest

# Prompts for missing values, prefilled with defaults
hyperctl create

# Non-interactive
hyperctl create --release 5.1.0-ec.0 --name "$USER-karpenter" --region eu-central-1

export KUBECONFIG=$(hyperctl kubeconfig mgmt)
```

`hyperctl create --help` lists all flags and defaults.

## Files

hyperctl follows the XDG Base Directory Specification on macOS and Linux:

| Directory | Default | Content |
|---|---|---|
| Config | `~/.config/hyperctl/config.yaml` | User defaults, see below |
| Data | `~/.local/share/hyperctl` | Installer directories and kubeconfigs, needed to destroy clusters |
| State | `~/.local/state/hyperctl` | Current cluster, last used release |
| Cache | `~/.cache/hyperctl` | `openshift-install` binaries per release |

Config values override built-in defaults, flags override config values. The
SSH key is asked once and saved; `sshKey: ""` disables it.

```yaml
region: us-east-1
baseDomain: autoscaling-dev.devcluster.openshift.com
pullSecret: ~/.openshift/pull-secret-latest.json
sshKey: ~/.ssh/id_ed25519.pub
```

## Development

```sh
go test ./...
golangci-lint run ./...
```

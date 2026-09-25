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
| Data | `~/.local/share/hyperctl` | Installer directories and kubeconfigs, needed to destroy clusters |
| State | `~/.local/state/hyperctl` | Current cluster |
| Cache | `~/.cache/hyperctl` | `openshift-install` binaries per release |

## Development

```sh
go test ./...
golangci-lint run ./...
```

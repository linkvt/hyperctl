# hyperctl

> [!NOTE]
> This code base is heavily AI-generated.

Interactive CLI that creates clusters for Karpenter development on AWS or Azure: a HyperShift management cluster with an AutoNode hosted cluster, or just parts of it. On AWS the management cluster is OpenShift, on Azure it's AKS. hyperctl remembers the clusters it creates and keeps a current cluster, like a kubectl context, so most commands need no cluster name.

<!-- TODO: asciinema recording of hyperctl create, docs/assets/create.gif -->

## Prerequisites

- Go, the version in [go.mod](go.mod)
- `oc`
- `hypershift`, built from [openshift/hypershift](https://github.com/openshift/hypershift) with `make build`. Not needed for clusters without HyperShift.
- A pull secret, by default `~/.openshift/pull-secret-latest.json`. Not needed for an AKS cluster without HyperShift.

## Installation

```sh
go install github.com/linkvt/hyperctl@latest
```

From a checkout: `go install .`. Both install into `$(go env GOPATH)/bin`, which needs to be in `PATH`.

## Quick start

Before `create`, set up access to the cloud:

- **AWS:** static IAM user credentials, also in the `[default]` profile of `~/.aws/credentials`. Temporary credentials, e.g. from `rh-aws-saml-login`, don't work. [Details](docs/aws.md#prerequisites)
- **Azure:** `az login` to the subscription to use, with permission to create resource groups and AKS clusters, e.g. Contributor; for HyperShift also role assignments, e.g. Owner, and app registrations in Entra ID. [Details](docs/azure.md#prerequisites)

```sh
hyperctl create                           # asks for everything, takes about an hour on AWS
export KUBECONFIG=$(hyperctl kubeconfig)  # hosted and management cluster, hosted is current
oc get nodes
hyperctl status
hyperctl destroy
```

## Testing a Karpenter change

```sh
# Build and push your image with a unique tag, e.g. quay.io/<you>/karpenter-provider:<unique-tag>
hyperctl image karpenter-provider quay.io/<you>/karpenter-provider:<unique-tag>
hyperctl status                           # lists the overrides
hyperctl image karpenter-provider --reset # back to the release image
```

`karpenter-operator` overrides the operator the same way. Details: [Image overrides](docs/usage.md#image-overrides).

## Commands

| Command | Purpose |
|---|---|
| `hyperctl create` | Create a cluster, or add the HyperShift operator or a hosted cluster to one |
| `hyperctl list` | List the clusters, `*` marks the current one |
| `hyperctl use <name>` | Make a cluster the current one |
| `hyperctl status` | Show the current cluster and its management or hosted clusters |
| `export KUBECONFIG=$(hyperctl kubeconfig)` | Use the kubeconfigs of the current cluster |
| `hyperctl image karpenter-operator <image>` | Override the karpenter-operator image of the hosted cluster |
| `hyperctl image karpenter-provider <image>` | Override the Karpenter image of the hosted cluster |
| `hyperctl destroy` | Destroy the current cluster |
| `hyperctl destroy shared --platform aws` | Destroy the shared resources of a platform, `aws` or `azure` |
| `hyperctl releases [stream]` | List OpenShift releases |
| `hyperctl cache clean` | Remove cached installer binaries |

`status`, `kubeconfig`, `image` and `destroy` act on another cluster with `--cluster <name>`. Flags and defaults: `hyperctl <command> --help`.

## Documentation

- [Concepts](docs/concepts.md): cluster model, names, current cluster, lifecycle
- [Usage](docs/usage.md): create, kubeconfig, status, image overrides, destroy
- [AWS](docs/aws.md) and [Azure](docs/azure.md): prerequisites, shared resources
- [Configuration](docs/configuration.md): config file, precedence, directories
- [Troubleshooting](docs/troubleshooting.md)
- [Contributing](CONTRIBUTING.md)

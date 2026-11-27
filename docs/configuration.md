# Configuration

## Precedence

For each value, the first one set wins:

1. Flag
2. Interactive prompt (prefilled with the value below)
3. Existing cluster, when adding to it: region, base domain, pull secret, release
4. Config file
5. Last used value: release, setup, region per platform
6. Built-in default, shown in `hyperctl create --help`

## Config file

`~/.config/hyperctl/config.yaml`, optional. hyperctl writes two keys itself: `sshKey` the first time `create` asks for an SSH key, and `hypershift` when it asks for the binary's path.

```yaml
pullSecret: ~/.openshift/pull-secret-latest.json
sshKey: ~/.ssh/id_ed25519.pub   # "" for no SSH key
hypershift: ~/repos/openshift/hypershift/bin/hypershift
region: eu-central-1
azure:
  location: germanywestcentral
```

| Key | Sets |
|---|---|
| `pullSecret` | Pull secret file |
| `sshKey` | SSH public key for node access, `""` for none |
| `hypershift` | Path of the `hypershift` binary |
| `region`, `baseDomain` | AWS region and base domain |
| `oidcBucket` | S3 bucket for the OIDC documents on AWS |
| `mgmtInstanceType`, `mgmtWorkers` | Size of new AWS clusters |
| `azure.location`, `azure.baseDomain` | Azure location and base domain |
| `azure.baseDomainResourceGroup` | Resource group of the base domain's DNS zone |
| `azure.resourceGroup`, `azure.oidcStorageAccount`, `azure.dnsZone` | Names of the [shared Azure resources](azure.md#shared-resources) |
| `azure.mgmtVMSize`, `azure.mgmtNodes` | Size of new AKS clusters |

## Directories

hyperctl follows the XDG Base Directory Specification on macOS and Linux, so `XDG_*_HOME` variables move these directories.

| Directory | Default | Content |
|---|---|---|
| Config | `~/.config/hyperctl` | `config.yaml` |
| Data | `~/.local/share/hyperctl` | Per cluster in `clusters/<name>`: record, install directory and kubeconfig; hosted clusters in `clusters/<name>/hosted/<hosted name>`. Azure credentials and signing key in `azure/`. |
| State | `~/.local/state/hyperctl` | `state.yaml`: current cluster, last used release, setup and regions |
| Cache | `~/.cache/hyperctl` | `openshift-install` binaries per release |

## hypershift binary

hyperctl takes `hypershift` from `--hypershift`, the config file or `PATH`. If none is found in a terminal, it asks for the path and saves it. Build the binary from [openshift/hypershift](https://github.com/openshift/hypershift) with `make build`, and rebuild it to pick up HyperShift changes; `hyperctl create --setup management` then updates the operator of an existing cluster.

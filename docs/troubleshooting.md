# Troubleshooting

Most errors say what to do next. This page covers the cases that need more context.

## Where to look

- `hyperctl status` shows the phase of each cluster, whether the APIs respond, and the next step for incomplete clusters.
- Output of the wrapped tools is shown with a prefix, e.g. `[openshift-install]` or `[hypershift]`.
- The full `openshift-install` log: `~/.local/share/hyperctl/clusters/<name>/install/.openshift_install.log`.

## A create failed or was interrupted

```
previous installation of management cluster <name> did not complete, destroy it first
```

hyperctl doesn't resume a creation that didn't finish. Destroy the incomplete cluster, then run `create` again:

```sh
hyperctl destroy --cluster <name>
```

If only the hosted cluster failed, destroy the hosted cluster. The management cluster is kept and `create` adds a new hosted cluster to it.

## The management cluster is unreachable

`status` shows `API: unreachable`, typically because the cloud account's cleanup pruned the instances. Recreate the cluster:

```sh
hyperctl destroy --cluster <name> && hyperctl create --name <name>
```

`destroy` still deletes the cloud resources of its hosted clusters, using the infra IDs that hyperctl recorded.

## The release image can't be pulled

`create` checks the pull secret before creating anything. For CI release images (`registry.ci.openshift.org`) it prints how to add a CI registry token to the pull secret:

1. Copy the `oc login` command for the CI cluster from the URL in the message and run it.
2. `oc registry login --registry registry.ci.openshift.org --to <pull secret>`

The token expires, so repeat this when the check fails again.

## AWS credentials are rejected

`openshift-install` and the HyperShift operator need IAM user credentials. If you're in an `rh-aws-saml-login` shell, leave it and unset the `AWS_*` session variables. See [AWS prerequisites](aws.md#prerequisites).

## DNS leftovers on AWS

Clusters whose instances were pruned can leave Route53 records behind, and they block a new cluster with the same name. Before creating clusters, and after destroying them, hyperctl lists the records of clusters with those names and deletes them once you confirm. Without a terminal, pass `--yes` to delete them.

## Hosted kubeconfig not available yet

The hosted cluster is still being created and hasn't published its kubeconfig yet. Wait and retry; `hyperctl kubeconfig` without `--cluster` prints the management cluster's kubeconfig meanwhile.

## An image override doesn't show up

- Push every build with a unique tag. Setting the same image again only restarts the pods, and nodes may run the cached image.
- The cluster must be able to pull the image.
- Overrides need the standalone karpenter-operator, which `create --standalone-karpenter-operator=false` turns off.

## Azure: service principal does not exist

```
service principal <id> of ~/.local/share/hyperctl/azure/credentials.json does not exist, delete the file to create a new one
```

Someone deleted the service principal, but its credentials file is still there. Delete the file; the next `create` creates a new service principal.

## Azure: VM size not offered or restricted

The location doesn't offer the VM size to your subscription. Pick another location with `--region` or another size with `--instance-type`.

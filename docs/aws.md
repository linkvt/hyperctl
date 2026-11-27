# AWS

On AWS, hyperctl installs OpenShift with `openshift-install` and creates hosted clusters with `hypershift`.

## Prerequisites

- An AWS IAM user's credentials. Temporary credentials, e.g. from `rh-aws-saml-login`, are rejected.
  - hyperctl and `openshift-install` use the default AWS credential chain (`AWS_PROFILE`, `AWS_*` variables, `~/.aws`).
  - With HyperShift, the operator writes OIDC documents to S3 with the `[default]` profile of `~/.aws/credentials` (or `AWS_SHARED_CREDENTIALS_FILE`). That profile must hold IAM user credentials as well.
- A public Route53 zone for the base domain, by default `autoscaling-dev.devcluster.openshift.com`.
- With HyperShift, the EC2 Spot service-linked role in the account. `create` checks for it and prints the command to create it if it's missing.

## Cluster size

The size prompt offers the standard OpenShift cluster (3 control plane and 3 worker nodes across the zones of the region) and compact clusters with 1 worker in one zone. `--workers 0` or `1` also makes a compact cluster.

## Shared resources

With HyperShift, `create` creates an S3 bucket for the OIDC documents of hosted clusters if it's missing, by default `$USER-hypershift-oidc`. Its objects are publicly readable.

`hyperctl destroy shared --platform aws` keeps the bucket and the EC2 Spot service-linked role, which belongs to the whole account. Delete the bucket by hand if you no longer need it.

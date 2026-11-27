# AGENTS.md

hyperctl is an interactive Go CLI that creates clusters for Karpenter development on AWS (OpenShift) or Azure (AKS): standalone, with the HyperShift operator, and with AutoNode hosted clusters; it tracks several clusters and a current one. `docs/concepts.md` defines the domain terms (platform, setup, management/hosted cluster, shared resources, current cluster); use them in code and docs. `tmp/design.md` and `tmp/architecture/` (gitignored, may be missing) hold design notes and the backlog.

## Layout

- `internal/app`: CLI commands (urfave/cli v3), prompts (huh), flow, cluster records and state. Platform-specific create and destroy steps live behind the `platform` and `platformCreate` interfaces in `platform.go`, implemented in `aws*.go` and `azure*.go`.
- `internal/awsaccount`, `internal/azureaccount`: AWS and Azure SDK adapters behind the `app.AWSAccount` and `app.AzureAccount` interfaces.
- `internal/ocp`, `internal/hypershift`, `internal/oc`: wrappers that run the `openshift-install`, `hypershift` and `oc` binaries.
- `internal/process`: runner the wrappers start processes through; `processtest` records them in tests.
- `internal/releases`: release controller API client.
- `internal/oidc`: signing key and OIDC discovery documents of Azure hosted clusters.
- `internal/tui`: adaptations of huh fields.
- `internal/xdg`: config, data, state and cache directories.

## Conventions

- Every value `create` prompts for has a flag. With `--yes` or without a terminal nothing prompts (`prompts()` in `app.go`): values fall back to defaults, and missing required values fail.
- Flags that don't apply to the platform or setup are rejected before any prompt or cloud call.
- Cluster records (`cluster.yaml`) and `state.yaml` persist across hyperctl versions. Records written by earlier versions must keep loading and destroying, see `clusterRecord.hasOperator`.

## Documentation

User-visible changes update `--help` and the docs in the same commit. `--help` is the reference for flags and their defaults, the docs name flags only where behaviour depends on them; `README.md` and `docs/` explain behaviour: `usage.md` the commands, `aws.md`/`azure.md` prerequisites and shared resources per platform, `configuration.md` config keys and directories, `troubleshooting.md` failure cases. Write concise docs for users: overview first, then details; tables over prose. Leave out implementation details users don't act on, such as substeps of create and their durations.

## Testing

Work test-first. Tests drive the CLI in-process (`app.New(deps).Run`) with an isolated HOME and fakes of everything in `app.Deps`, see `internal/app/helpers_test.go` and `fake_*_test.go`. The fakes share state like the real systems (the fake hypershift creates hosted clusters in `fakeCluster`), record operations as typed values and fail through typed error fields; extend them for new behaviour and assert operations and their parameters as Go values. Command lines of the wrapped binaries are tested where they are built: the in-package tests of `internal/hypershift`, `internal/oc` and `internal/ocp` run commands through a `processtest.Recorder` and assert the exact argv and environment. Prompts need a TTY and are verified by hand.

A built hyperctl acts on the user's real clusters through the default XDG directories. Run it only with isolated `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` set on the same command, e.g. through a wrapper script. It then doesn't see the user's clusters: create clusters only with names that are not the defaults and none of the user's clusters use, as `create` deletes the DNS records of clusters it doesn't know as leftovers, without asking under `--yes`.

Behaviour against real systems (AWS and Azure adapters, release controller, CLI flags of the wrapped binaries) is checked with throwaway tests in `tmp/_smoke/`, which `go test ./...` skips. Real AWS resources created there get unique names and are deleted in the same run. Creating clusters costs money and about an hour, run it only when asked.

## Checks and commits

- Run `make verify` before committing. CI runs `make verify-ci`, which fails on unformatted code instead of rewriting it and runs the tests with the race detector.
- Conventional commits with DCO sign-off (`git commit -s`), one feature per commit.

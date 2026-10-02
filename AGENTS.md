# AGENTS.md

hyperctl is an interactive Go CLI that creates a HyperShift management
cluster plus an AutoNode hosted cluster on AWS for Karpenter development.
README.md covers usage; `tmp/design.md` (gitignored, may be missing) holds
design notes and the backlog.

## Layout

- `internal/app`: CLI commands (urfave/cli v3), prompts (huh), flow, state.
- `internal/awsaccount`: AWS SDK adapter behind the `app.AWSAccount`
  interface.
- `internal/ocp`, `internal/hypershift`, `internal/oc`: wrappers that run the
  `openshift-install`, `hypershift` and `oc` binaries.
- `internal/releases`: release controller API client.
- `internal/xdg`: config, data, state and cache directories.

## Testing

Work test-first. Tests drive the CLI in-process (`app.New(...).Run`) with an
isolated HOME, fake `oc`/`openshift-install`/`hypershift` shell scripts on
PATH and an in-memory `fakeAWS`, see `internal/app/helpers_test.go`. Extend
the fakes rather than mocking internal packages. Prompts need a TTY and are
verified by hand.

A built hyperctl acts on the user's real clusters through the default XDG
directories. Run it only with isolated `XDG_CONFIG_HOME`, `XDG_DATA_HOME`,
`XDG_STATE_HOME` and `XDG_CACHE_HOME` set on the same command, e.g. through a
wrapper script.

Behaviour against real systems (AWS adapter, release controller, CLI flags of
the wrapped binaries) is checked with throwaway tests in `tmp/_smoke/`, which
`go test ./...` skips. Real AWS resources created there get unique names and
are deleted in the same run. Creating clusters costs money and about an hour,
run it only when asked.

## Checks and commits

- Run `make verify` before committing.
- Conventional commits with DCO sign-off (`git commit -s`), one feature per
  commit.

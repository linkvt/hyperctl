# Contributing

[AGENTS.md](AGENTS.md) is the contributor guide for humans and agents alike: code layout, testing, checks and commits. This file adds what only humans need.

## Tools

- Go, the version in [go.mod](go.mod)
- [golangci-lint](https://golangci-lint.run) v2, the version pinned in [CI](.github/workflows/ci.yml)

```sh
make verify   # format, test, lint
```

## Running a local build

A built hyperctl acts on your real clusters through the default directories. Point it at separate directories instead, e.g. with a script you source from the repository root:

```sh
work=$(pwd)/tmp/dev
export XDG_CONFIG_HOME="$work/config" XDG_DATA_HOME="$work/data" XDG_STATE_HOME="$work/state" XDG_CACHE_HOME="$work/cache"
mkdir -p "$work/bin" && go build -o "$work/bin/hyperctl" . && export PATH="$work/bin:$PATH"
```

The cloud accounts are still real. A local build doesn't see the clusters in your default directories: give the clusters you create with it names that your other clusters don't use, otherwise `create` treats their DNS records as leftovers and offers to delete them. `create` costs money and takes about an hour, so destroy what you create. Interactive prompts need a terminal and are tested by hand.

## Documentation

User docs are [README.md](README.md) and [docs/](docs). `--help` is the reference for flags and their defaults; the docs explain behaviour and name flags only where it depends on them.

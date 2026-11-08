# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Sensu Go check reading the nginx `stub_status` page, written in Go with `github.com/sensu/sensu-plugin-sdk`. Published as a Sensu asset (static binary, `linux/amd64` only, see `ARCHS` in the Makefile). Same layout and tooling as `opsone/sensu-plugins-systemd` (single binary, `main.go` at the root).

## Common Commands

```bash
make test          # go vet + unit tests
make build         # local binary at the root
make integration   # Docker: real nginx (integration/nginx.conf), check run as nobody
make dist          # tests, then tar.gz per arch + sha512 + asset YAML in dist/
make release       # dist + GitHub release of the current tag (gh)
```

Version comes from `git describe --tags` (override with `VERSION=`), injected via ldflags into the SDK `version` package.

## Architecture

- `main.go`: options (`CHECK_NGINX_*` env vars), HTTP GET with `net/http` (redirects not followed), then pure functions `parseStubStatus` and `evaluate` (tested in `main_test.go`).
- States: OK; WARNING for thresholds exceeded (strictly greater), 3xx or a 200 that is not a stub_status page; CRITICAL for other HTTP codes and connection errors; UNKNOWN for invalid arguments.
- `--url` is required with no default in the binary: the default lives in the CheckConfig (`| default "http://127.0.0.1/nginx-status"`).
- `-h` is reserved by cobra for `--help` (redefining it panics).

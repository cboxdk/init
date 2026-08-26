---
title: "Testing"
description: "How Cbox Init is tested: Go unit tests with race + coverage, and Docker-based integration tests"
weight: 51
---

# Testing

Cbox Init is a Go project. Its tests are Go unit tests plus a Docker-based
integration suite. There is no web UI and no browser/Playwright test suite.

## Unit tests

Run the unit tests with the race detector and coverage (this is what CI runs):

```bash
make test          # go test -v -race -coverprofile=coverage.out ./...
```

Every package has `_test.go` files alongside the code (for example
`internal/process/*_test.go`, `internal/signals/handler_test.go`,
`internal/schedule/*_test.go`, `internal/config/*_test.go`). Coverage should
stay **above 80%**; new behaviour needs tests.

Useful variants:

```bash
go test ./internal/process        # a single package
go test -run TestName ./...        # a single test
go test -race -short ./...         # race detector, short mode (also a CI step)
make coverage                      # write coverage.html
make bench                         # benchmarks
```

Environment-gated tests (Linux cgroups, root-only paths) skip cleanly on macOS
and are exercised on the Linux leg in CI.

## Lint and the full gate

```bash
make lint     # golangci-lint, pinned to v2.12.2 (matches CI; config in .golangci.yml)
make check    # the whole gate: fmt-check, vet, lint, test, vulncheck, sbom-check, license-check
```

`make check` mirrors CI, so run it before pushing.

## Integration tests (Docker)

The integration suite builds the binary into Alpine, Debian, and Ubuntu images
and runs it as PID 1, checking startup, config validation, the metrics endpoint,
and the API:

```bash
make test-integration   # builds and runs tests/integration/Dockerfile.{alpine,debian,ubuntu}
```

The test script is `tests/integration/run-tests.sh` and the sample config is
`tests/integration/test-config.yaml`. Requires Docker.

## Full functional suite

```bash
make test-all           # runs tests/run-all-tests.sh
```

`tests/run-all-tests.sh` runs the unit tests, the race detector, multi-platform
builds, and binary sanity checks in sequence. Requires Docker for the
integration portions.

## Continuous integration

`.github/workflows/ci.yml` runs on pushes and pull requests:

- **Test** — `make test` and `go test -race -short ./...` on Ubuntu and macOS,
  plus `make fmt-check` and a `go mod tidy` drift check; uploads coverage.
- **Supply chain** — `govulncheck`, `make sbom-check`, `make license-check`.
- **Lint** — `golangci-lint` (v2.12.2).
- **Build** — `make build-all` for Linux/macOS × amd64/arm64.
- **Integration Test** — the Docker suite on alpine, debian, and ubuntu.

Documentation config examples are validated by a separate check; see
[Validation](../configuration/validation) and `tools/check-doc-configs.sh`.

## See Also

- [Contributing](https://github.com/cboxdk/init/blob/main/CONTRIBUTING.md) - Local workflow and standards
- [Validation](../configuration/validation) - `check-config` and how configs are checked

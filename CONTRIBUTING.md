# Contributing to Cbox Init

Thanks for helping improve Cbox Init — the process supervisor that runs as PID 1
inside Cbox PHP base images. This guide covers the local workflow so you can get
a change reviewed quickly.

## Prerequisites

- **Go 1.24+** (`go version`)
- **Docker** — only for the integration/functional suites (`make test-integration`, `make test-all`)
- **golangci-lint v2.12.2** — matches CI. Install: https://golangci-lint.run/welcome/install/

## Build & run

```bash
make build                 # build ./build/cbox-init (version derived from git)
make dev                   # build and run against configs/examples/minimal.yaml
./build/cbox-init version  # sanity-check the version string
make help                  # list all targets
```

## Tests & lint (run before every PR)

```bash
make test    # unit tests with -race and coverage
make lint    # golangci-lint, identical to CI (config in .golangci.yml)
```

Optional, Docker-backed:

```bash
make test-integration   # runs on alpine, debian, ubuntu images
make test-all           # full functional/API suite
make coverage           # HTML coverage report
```

Coverage should stay **above 80%**. New behaviour needs tests — unit tests for
logic, and where it touches process lifecycle/signals, a deterministic test
using the mockable seams (see `internal/signals/handler_test.go` and
`internal/process/*_test.go`). Environment-gated tests (Linux cgroups, root)
skip cleanly on macOS; CI exercises them on the Linux leg.

## Coding standards

- Run `gofmt` (CI enforces formatting).
- Keep changes focused; match the style of the surrounding package.
- Prefer explicit error handling. When ignoring an error is correct (best-effort
  cleanup), write `_ = f()` with a short reason rather than dropping it silently.
- This process runs as **PID 1** — be especially careful with signal handling,
  zombie reaping, goroutine lifecycles, and anything that can wedge the event
  loop or leak file descriptors.

## Commits & PRs

- Use clear, conventional commit subjects (`fix(process): …`, `feat(api): …`,
  `docs: …`).
- Update `CHANGELOG.md` (Keep a Changelog format) for user-facing changes.
- Update the relevant docs under `docs/` and, if you add a config field, the
  configuration reference.
- Describe the behaviour change and how you verified it in the PR body.

## Releases

Releases are tag-driven (`vX.Y.Z`). The release workflow injects the version
from the tag, builds multi-arch binaries + images, and publishes checksums. Do
not hand-edit version strings — `make build` derives the version from git.

## Security

Please report vulnerabilities privately — see [SECURITY.md](SECURITY.md). Do not
open public issues for security problems.

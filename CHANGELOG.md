# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **`check-config --json` now emits real JSON.** The flag advertised for CI/CD
  hand-rolled its serialization and printed Go syntax instead — a `[{ … map[errors:0 …] }]`
  dump that no `jq` pipeline or JSON parser could read, and the error path
  interpolated unescaped error text so a multi-line YAML error produced broken
  JSON too. Output now goes through `json.MarshalIndent`, round-trips through any
  JSON parser, and load/validation failures print a properly-escaped
  `{"error":"…"}`. The documented schema in `configuration/validation.md` was
  fictional (`valid`/`recommendation`/`counts`); it now matches the real output.
- **Graceful shutdown now actually happens.** Every managed process is launched
  with `exec.CommandContext`, and `Supervisor.Stop` cancelled that context
  *before* running the pre-stop hook and sending the configured shutdown signal.
  Go's os/exec watchdog SIGKILLs a child the instant its context is cancelled, so
  on every stop path — `docker stop`, `cbox-init stop`/`restart`, hot-reload,
  scale-to-zero — the child was force-killed before it ever saw SIGTERM. The
  configured `shutdown.signal`, pre-stop hooks, graceful timeout, and SIGKILL
  escalation were all dead code; PHP-FPM and nginx lost in-flight requests on
  every deploy. Context cancellation no longer kills children (`cmd.Cancel` is a
  no-op); `stopInstance` is the sole stop authority (pre-stop hook → configured
  signal → timeout → SIGKILL), and aborted-start cleanup force-kills survivors
  explicitly. Regression tests now assert a child actually *receives* SIGTERM and
  that a child ignoring it is force-killed after the timeout — the assertion no
  test made before, which is why this survived.

### Added

- Global lifecycle hooks can be defined entirely via environment variables —
  `CBOX_INIT_HOOK_<TYPE>_<N>_<FIELD>` (e.g. `CBOX_INIT_HOOK_PRE_START_0_COMMAND=php,please,stache:warm`)
  — so docker-compose/k8s deployments on prepared base images need no YAML
  mount for warmup or migration hooks. Env-defined hooks append after
  YAML-defined ones, ordered by index; `ALLOW_FAILURE` is accepted as the env
  spelling of `continue_on_error`. (#33)
- `COMMAND` env overrides (hooks and processes) accept a comma-separated list
  alongside the existing JSON-array form.
- Hooks are validated at config load (command required, non-negative
  timeout/retry/retry_delay) and unnamed hooks get a stable default name
  (`pre-start-0`, …) so logs and metrics always carry a hook identity.
- Hook execution logs carry structured `type`, `duration_seconds` and
  `exit_code` fields.

- The supply-chain gate the sibling packages run: `govulncheck`, a deterministic
  CycloneDX 1.5 SBOM (`tools/sbomnorm`) and a dependency license check
  (`tools/licensecheck`), plus gofmt and `go mod tidy` drift checks. CI had none
  of these, so nothing would have reported a vulnerable dependency or a
  non-permissive licence. 63 dependencies, all permissive.
- `make check` runs the whole gate locally, identical to CI.
- `bodyclose`, `errorlint` and `misspell`, added one at a time per this repo's
  own linter policy after fixing everything they surfaced.

### Fixed

- 26 files were not gofmt-clean; CI had no formatting gate to notice.
- `handleExecutionError` used a type assertion on the error, and two comparisons
  used `==`/`!=`, all of which stop matching once an error is wrapped.
- Stale PHPeek branding in the TUI header and keyboard-shortcut screen, the
  audit log's start and shutdown records, the build-info metric's help text and
  a configuration warning.


## [2.5.1] - 2026-08-11

### Fixed

- **Stopping a process that has already exited is no longer reported as a failure.**
  The signal races the child's own exit: on a loaded machine the process can go
  between the liveness check and the kill, `Process.Signal` returns
  `os.ErrProcessDone`, and the stop came back "failed to send signal". `scale 0`
  then reported an error for work that was already done. `os.ErrProcessDone` and
  `ESRCH` now mean what they say; `EPERM` still fails.
- **Dependencies swept to current** — gopsutil 4.26.7, client_golang 1.24.1,
  fsnotify 1.10.1, cobra 1.10.2, otel 1.45.0, bubbles 1.0.0, alpine 3.24, and
  every GitHub Action to its current major.
- **CI could not have caught either.** `syscall.Getsid` does not exist on Linux,
  so the test build failed on the only platform this ships on, and
  golangci-lint-action v6 refuses a v2 golangci-lint, so the lint job never ran.

## [2.5.0] - 2026-08-11

### Added

- **Snapshot: checkpoint and restore a running container's processes.** A coordinator
  quiesces supervised children, owns their stdio across the checkpoint so a restored
  process keeps writing to the same pipes, and treats a checkpointed exit as a
  checkpoint rather than a crash — no restart storm on resume. The warm tier survives
  repeated cycles and recovers from an interrupted one.
- **Engine-aware autotuning for Percona and Valkey.** Sizing is derived from the
  container's real memory and CPU limits and applied at startup, including InnoDB's
  buffer-pool rounding, which previously made the applied value disagree with the
  computed one.

### Fixed

- **Every Cbox base image shipped a binary with a CRITICAL CVE.** `cbox-init` linked
  grpc v1.75.0 (CVE-2026-33186, improper HTTP/2 handling; plus GHSA-hrxh-6v49-42gf)
  and was built with Go 1.24, whose stdlib carries eight more across `net/url`,
  `crypto/x509`, `crypto/tls` and `net/http`. Trivy had been reporting this on every
  image build, but the scan runs after the push, so it never stopped a publish.
  grpc 1.83.0, `x/net` 0.57.0, `x/text` 0.40.0, and the toolchain moves to Go 1.26 in
  go.mod, both workflows and the Dockerfile.
- **Go MPTCP is disabled so the process is checkpointable.** CRIU cannot parasite-inject
  through the MPTCP sockets Go dials by default, which made scale-to-zero
  suspend/resume fail on any container running cbox-init as PID 1.

## [2.4.1] - 2026-07-15

### Fixed
- Documentation restructured for findability and dead links fixed, including the PHP-FPM auto-tuning guide, which is now reachable from the environment-variables reference.

## [2.4.0] - 2026-07-10

### Fixed

- **PID-1 zombie reaper no longer races with the supervisor's own `Wait()`.** The wildcard
  reaper (`Wait4(-1)`) could reap a supervised child before its `cmd.Wait()` ran, leaving a nil
  `ProcessState` that panicked `monitorInstance` and left the process permanently in `failed`
  (never restarted, `/readyz` stale). Supervisors now register their PIDs; a racing reaper stashes
  the exit status for the supervisor to recover, so exit codes are preserved and restarts always run.
- **Restart budget is now a sliding window, not a lifetime total.** `restartCount` accumulated for
  the life of the container, so a service that crashed more than `max_restart_attempts` times — even
  with days of healthy uptime between crashes — was abandoned forever. A new
  `global.restart_stability_window` (default `60s`) resets the budget once an instance stays up long
  enough to be considered recovered (systemd `StartLimitIntervalSec` semantics; negative disables).
- **Log tailer no longer leaks a file descriptor on every rotation.** A `defer file.Close()` inside
  the reopen path accumulated one open FD per rotation, eventually exhausting `RLIMIT_NOFILE` on
  high-rotation logs.
- **Shutdown no longer skips processes when the dependency graph is invalid.** If a reload swapped in
  a config whose dependencies no longer resolve, `getShutdownOrder` returned an empty list and left
  children to be SIGKILLed by the runtime; it now falls back to stopping all known processes.

### Security

- **Rate limiter can no longer be bypassed via `X-Forwarded-For` spoofing.** It now keys on the
  real client IP (RemoteAddr), only honoring `X-Forwarded-For` when `trust_proxy` is enabled —
  previously a client could send a unique header per request to mint a fresh token bucket and defeat
  rate limiting (and brute-force the API token).
- Config files are now written `0600` (was `0644`). A saved config can contain the management-API
  bearer token (`api_auth`), so it must not be world-readable by other UIDs in the container.
- Recursive ownership fixes use `Lchown` instead of `Chown`, so a symlink planted in a user-writable
  mounted volume (storage/, wp-content/) can't redirect the chown at an arbitrary target file.

### Added

- **`api_host` / `metrics_host`** settings to restrict the API and metrics listeners to a specific
  interface (e.g. `127.0.0.1`). Default is unchanged (all interfaces), so this is opt-in hardening.

### Fixed (reload)

- **Config reload now validates the new config before stopping anything.** An invalid config (bad
  settings or a dependency cycle) previously stopped the removed/changed services first and only then
  failed, leaving them down. A failed reload is now a no-op — the running configuration is untouched.

### Changed

- Local `make build` now derives the version from `git describe` instead of a hardcoded `1.0.0`, so
  locally built binaries report the real version. `make dev` runs against `configs/examples/minimal.yaml`.
- Added `.golangci.yml` (v2), pinned `golangci-lint` in CI, `SECURITY.md`, `CONTRIBUTING.md`, and
  Dependabot for Go modules, GitHub Actions, and Docker.

## [2.3.1] - 2026-07-08

### Fixed

- **Supervised-process stdout/stderr now actually reaches container stdout.** Info-level output
  from tracked processes (php-fpm, nginx, queue workers, and JSON- or file-tailed lines detected
  as info) was routed through `slog.Debug()` in the process-log pipeline and therefore silently
  dropped by the default `log_level: info` handler — only `warn`/`error` survived. The result: a
  container's normal application logs never appeared in `kubectl logs` / `docker logs`, and a
  Laravel error logged via `LOG_CHANNEL=stderr` was invisible. Info-level process entries are now
  emitted at info, restoring the documented default that `logging.stdout` / `logging.stderr`
  forward process output. (The `default` switch arm was likewise corrected from debug to info.)

## [2.3.0] - 2026-07-07

### Added

- **Active HTTP readiness/liveness endpoints** — set `global.readiness.http_port` to expose:
  - `GET /readyz` → `200` when all tracked processes are ready, `503` otherwise (JSON body lists
    each tracked process's state). Drives the Kubernetes `readinessProbe`.
  - `GET /livez` → `200` whenever cbox-init can answer. Drives the `livenessProbe`.

  Unlike the readiness **file** (passive, can go stale if the supervisor wedges), these are served
  by cbox-init itself: a hung supervisor stops answering and the probe fails. The file remains for
  `exec` probes — the two are complementary. Bound to `0.0.0.0` by default so the kubelet can reach it.

## [2.2.0] - 2026-07-07

### Added

- **Startup performance controls** — new options to tune process manager startup behavior.

### Changed

- **Dependency-aware reload** — configuration reloads now respect process dependency ordering.
- **Secure runtime observability defaults** — metrics/observability endpoints now ship with safer, locked-down defaults.

### Fixed

- **`version` reported the wrong number on every release** — the version was a `const`, which the `-ldflags "-X main.version=..."` linker flag cannot override, so all builds reported `1.0.0`. It is now a `var` (default `dev`); release builds report the injected semver.
- **Health readiness semantics** — a process is now only reported ready when it is genuinely ready (fixes false-healthy reporting).
- **Hardened process lifecycle handling** — more robust start/stop/restart and supervisor edge-case handling.

## [2.1.1] - 2026-05-07

### Fixed

- **Permission setup: respect PUID/PGID and auto-detect www-data uid** — Framework directory ownership (`storage/`, `var/`, `wp-content/`) previously hardcoded uid/gid 82 (Alpine convention), which silently broke on Debian-based images where `www-data` is uid 33. The binary now resolves the app user via: (1) `PUID`/`PGID` env vars, (2) `/etc/passwd` lookup of `www-data`, (3) fallback to 82/82. This fixes Laravel 500 errors caused by view cache write failures on `php-fpm-nginx:*-bookworm` images.

## [2.1.0] - 2026-05-07

### Added

- CLI commands for process control (`list`, `status`, `start`, `stop`, `restart`, `scale`, `reload-config`, `logs`)
- Always-on Unix socket for CLI-to-daemon communication
- Log file tailing with rotation support
- API client package (`internal/apiclient`) extracted from TUI
- Log subscriber system for real-time log streaming

## [2.0.1] - 2026-04-17

### Fixed

- Oneshot processes now default to `restart: never` instead of inheriting the global restart policy

## [2.0.0] - 2026-04-17

### Changed

- Rebranded from phpeek-pm to cbox-init

## [1.2.2]

### Added

- Scaffolding `--observability` flag and streamlined presets

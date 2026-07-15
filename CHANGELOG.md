# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

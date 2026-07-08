# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

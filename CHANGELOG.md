# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

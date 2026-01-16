# CLAUDE.md

Guidance for Claude Code when working with this repository.

## Project Overview

Cbox Init (cbox-init) is a production-grade PID 1 process manager for Docker containers, designed for PHP applications. Written in Go, it manages multiple processes (PHP-FPM, Nginx, queue workers, framework daemons) with proper signal handling, zombie reaping, health checks, and graceful shutdown. Works with Laravel, Symfony, WordPress, and any PHP framework.

**Status**: Production-ready. All core features implemented.

## Build & Development

```bash
# Build
make build              # Current platform → build/cbox-init
make build-all          # All platforms (Linux/macOS, AMD64/ARM64)
make dev                # Build and run locally
make clean              # Remove build artifacts

# Test
make test               # All tests with race detection and coverage
go test -v ./internal/process  # Specific package
go test -run TestName   # Specific test

# Dependencies
make deps               # Download and tidy
```

## Running

```bash
# Basic usage
./build/cbox-init                              # Auto-detects config
./build/cbox-init --config configs/examples/minimal.yaml
./build/cbox-init serve --watch                # Watch mode (hot-reload)

# Config priority: --config flag > CBOX_INIT_CONFIG env > /etc/cbox-init/cbox-init.yaml > cbox-init.yaml

# Validation
./build/cbox-init check-config                 # Full validation
./build/cbox-init check-config --strict        # Fail on warnings (CI/CD)
./build/cbox-init check-config --json          # JSON output

# TUI
./build/cbox-init tui                          # Local (Unix socket)
./build/cbox-init tui --url http://host:9180   # Remote (TCP)

# Scaffolding
./build/cbox-init scaffold laravel             # Generate Laravel config
./build/cbox-init scaffold --interactive       # Interactive mode
```

## Directory Structure

```
cmd/cbox-init/          # Main entry point
internal/
  config/               # Configuration loading and validation
  logger/               # Structured logging (slog)
  process/              # Process management core
    manager.go          # Multi-process orchestration
    supervisor.go       # Single process lifecycle
  signals/              # Signal handling and zombie reaping
  api/                  # REST API server
  tui/                  # Terminal UI
  scaffold/             # Config generators
  watcher/              # File watching for hot-reload
  schedule/             # Cron-like scheduler
  metrics/              # Prometheus metrics
  tracing/              # OpenTelemetry integration
configs/examples/       # Example configurations
docs/                   # Full documentation
```

## Architecture Principles

1. **Interfaces over Concrete Types** - Components accept interfaces, return structs
2. **Dependency Injection** - Pass dependencies through constructors, no globals
3. **Error Wrapping** - Use `fmt.Errorf("context: %w", err)` for error chains
4. **Context Propagation** - Pass `context.Context` for cancellation/timeouts
5. **Graceful Degradation** - Non-critical failures log warnings, don't crash

## Code Patterns

```go
// ✅ GOOD: Interface + DI + Error wrapping + Context
type ProcessManager interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}

func NewManager(cfg *config.Config, log *slog.Logger) ProcessManager {
    return &manager{config: cfg, logger: log}
}

func (m *manager) Start(ctx context.Context) error {
    if err := m.validateConfig(); err != nil {
        return fmt.Errorf("invalid config: %w", err)
    }
    // ...
}

// ❌ BAD: Globals, no error handling, no context
var Procs map[string]*Process
func StartAll() { for _, p := range Procs { p.Start() } }
```

## Key Components

- **Manager** (`internal/process/manager.go`): Orchestrates processes with DAG-based startup ordering
- **Supervisor** (`internal/process/supervisor.go`): Manages single process lifecycle with scaling
- **Scheduler** (`internal/schedule/scheduler.go`): Cron-like task scheduling
- **API** (`internal/api/`): REST API for process control
- **TUI** (`internal/tui/`): k9s-style terminal interface

## Adding Features

1. Define interfaces first
2. Add config structs in `internal/config/types.go`
3. Add validation in `config.Validate()`
4. Add defaults in `config.SetDefaults()`
5. Write tests (aim for >80% coverage)
6. Update examples in `configs/examples/`

## Documentation

Full documentation in [`/docs`](docs/):
- [Configuration](docs/configuration/overview.md)
- [Features](docs/features/) - Health checks, scaling, hooks, scheduled tasks
- [Observability](docs/observability/) - Metrics, API, tracing

## Minimal Config Example

```yaml
version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
```

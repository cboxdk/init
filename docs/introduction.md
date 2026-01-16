---
title: "Introduction"
description: "Production-grade PID 1 process manager for PHP applications in Docker containers"
weight: 1
---

# Cbox Init

Cbox Init is a production-grade process manager designed specifically for running PHP applications in Docker containers. Built in Go, it serves as PID 1, providing proper signal handling, zombie reaping, and graceful shutdown for multi-process orchestration.

## What is Cbox Init?

Cbox Init manages multiple processes within a single Docker container, making it ideal for PHP applications that need to run:

- PHP-FPM for web requests
- Nginx as a reverse proxy
- Queue workers for background job processing
- WebSocket servers for real-time communication
- Scheduled tasks (cron jobs)
- Framework-specific daemons (Horizon, Messenger, wp-cron, etc.)

## Why Cbox Init?

**PID 1 Capability**
- Proper signal handling (SIGTERM, SIGINT, SIGQUIT)
- Zombie process reaping
- Clean process tree management

**Multi-Process Orchestration**
- DAG-based dependency management
- Priority-based startup ordering
- Graceful shutdown with configurable timeouts
- Process scaling (run multiple instances)

**Health Monitoring**
- TCP, HTTP, and exec health checks
- Success thresholds to prevent restart flapping
- Automatic restart policies (always, on-failure, never)

**Production Ready**
- Prometheus metrics for monitoring
- REST API for runtime control
- Structured JSON logging
- Framework detection (Laravel, Symfony, WordPress)

## Who Should Use Cbox Init?

**Perfect For**
- PHP applications needing multiple services in one container
- Production deployments requiring observability
- Teams wanting simplified Docker orchestration
- Applications with queue workers and scheduled tasks
- Laravel, Symfony, WordPress, or any PHP framework

**Not Ideal For**
- Simple single-process applications
- Kubernetes environments (use native pod patterns instead)
- Development environments (use Docker Compose for multiple containers)

## Architecture Overview

```
┌─────────────────────────────────────────────────┐
│         Cbox Init (PID 1)                      │
│                                                 │
│  ┌──────────┐  ┌──────────┐  ┌─────────────┐ │
│  │ Metrics  │  │   API    │  │  Heartbeat  │ │
│  │ :9090    │  │  :9180   │  │  Monitor    │ │
│  └──────────┘  └──────────┘  └──────┬──────┘ │
│                                       │         │
│                            External monitoring │
│                            (healthchecks.io)   │
│                                                 │
│  ┌──────────────────────────────────────────┐ │
│  │     Process Manager & Orchestration      │ │
│  │  • DAG-based dependency resolution       │ │
│  │  • Health monitoring with thresholds     │ │
│  │  • Lifecycle hooks (pre/post start/stop) │ │
│  │  • Graceful shutdown with timeouts       │ │
│  └──────────────────────────────────────────┘ │
│                                                 │
│  ┌──────────────────────────────────────────┐ │
│  │     Advanced Logging Pipeline            │ │
│  │  Log → Level Detect → Multiline →        │ │
│  │  JSON Parse → Redact → Filter → Output   │ │
│  └──────────────────────────────────────────┘ │
│                                                 │
│  ┌──────────┐ ┌──────────┐ ┌────────┐        │
│  │ PHP-FPM  │ │  Nginx   │ │ Queue  │        │
│  │ (scale 2)│ │          │ │Workers │        │
│  └──────────┘ └──────────┘ └────────┘        │
│                                                 │
│  ┌──────────┐ ┌──────────────────────┐       │
│  │ WebSocket│ │   Cron Scheduler     │       │
│  │  Server  │ │  • Standard 5-field  │       │
│  │          │ │  • Task statistics   │       │
│  │          │ │  • Heartbeat pings   │       │
│  └──────────┘ └──────────────────────┘       │
└─────────────────────────────────────────────────┘
```

## Key Features

**Process Management**
- Multi-process orchestration with DAG-based dependency resolution
- Process scaling (run N instances of the same process)
- Restart policies with exponential backoff
- Lifecycle hooks (pre/post start/stop)

**Scheduled Tasks**
- Built-in cron scheduler with standard 5-field format
- Per-task execution statistics and metrics
- External heartbeat monitoring integration (healthchecks.io, Cronitor, etc.)
- Graceful cancellation on shutdown

**Health Monitoring**
- TCP port checks
- HTTP endpoint validation with status code verification
- Custom exec commands
- Configurable failure/success thresholds to prevent flapping

**Advanced Logging**
- Automatic log level detection (ERROR, WARN, INFO, DEBUG)
- Multiline log reassembly (stack traces, exceptions)
- JSON log parsing and structured field extraction
- Sensitive data redaction (passwords, tokens, PII)
- GDPR, PCI DSS, HIPAA, SOC 2 compliance support

**Observability**
- Prometheus metrics endpoint
- Process lifecycle metrics (start, stop, restart, exit codes)
- Health check duration and status tracking
- Hook execution metrics
- Scheduled task metrics (last run, next run, duration, success/failure)
- REST API for runtime inspection and control

**Framework Integration**
- Auto-detect Laravel, Symfony, WordPress
- Framework-specific permission setup
- Configuration validation
- Support for framework CLIs (artisan, console, wp-cli)

## Quick Example

```yaml
version: "1.0"

global:
  shutdown_timeout: 30
  log_level: info
  metrics_enabled: true
  api_enabled: true

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    restart: always

  # Laravel: php artisan queue:work
  # Symfony: php bin/console messenger:consume
  # Custom: php /app/worker.php
  queue-worker:
    enabled: true
    command: ["php", "artisan", "queue:work", "--sleep=3", "--tries=3"]
    depends_on: [php-fpm]
    scale: 3
    restart: always
```

## Next Steps

- [Installation](getting-started/installation) - Get Cbox Init installed
- [Quick Start](getting-started/quickstart) - 5-minute getting started guide
- [Configuration](configuration/overview) - Complete configuration reference
- [Examples](examples/) - Real-world configuration examples

## Community

- [GitHub Repository](https://github.com/cboxdk/init)
- [Issue Tracker](https://github.com/cboxdk/init/issues)
- [Discussions](https://github.com/cboxdk/init/discussions)

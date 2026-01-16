---
title: "Features"
description: "Comprehensive guide to Cbox Init features including health checks, scaling, and monitoring"
weight: 20
---

# Features

Cbox Init provides production-grade process management features designed for PHP applications in Docker containers. Whether you're running Laravel, WordPress, Symfony, or any PHP framework, these features help you manage multi-process containers reliably.

## Core Features

- [Dependency Management](dependency-management) - DAG-based startup ordering
- [Health Checks](health-checks) - TCP, HTTP, and exec-based monitoring
- [Container Readiness](container-readiness) - File-based K8s readiness probes
- [Scheduled Tasks](scheduled-tasks) - Built-in cron scheduler
- [Process Scaling](process-scaling) - Multi-instance worker management
- [Restart Policies](restart-policies) - Always, on-failure, never strategies
- [Advanced Logging](advanced-logging) - Multiline, redaction, JSON parsing
- [Heartbeat Monitoring](heartbeat-monitoring) - External monitoring integration

## Quick Overview

### Health Monitoring

```yaml
processes:
  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    health_check:
      type: tcp
      address: "127.0.0.1:80"
      interval: 10
      timeout: 5
      retries: 3
```

### Scheduled Tasks

```yaml
processes:
  backup-job:
    enabled: true
    command: ["php", "/app/scripts/backup.php"]
    schedule: "0 2 * * *"  # Daily at 2 AM
```

## See Also

- [Configuration Overview](../configuration/overview) - All configuration options
- [Examples](../examples/) - Real-world configurations

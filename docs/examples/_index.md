---
title: "Examples"
description: "Real-world Cbox Init configurations for PHP frameworks, Docker, and Kubernetes deployments"
weight: 30
---

# Examples

Practical, production-ready configuration examples for common use cases with PHP applications including Laravel, WordPress, Symfony, and more.

## Available Examples

### Core Examples

These work with any PHP framework:

- [Minimal Setup](minimal) - Simple PHP-FPM configuration
- [Node.js Applications](nodejs) - Next.js, Nuxt, Remix, Express, and other Node servers
- [Scheduled Tasks](scheduled-tasks) - Cron scheduler for Laravel, Symfony, WordPress
- [Kubernetes Deployment](kubernetes) - K8s deployment patterns
- [Docker Compose](docker-compose) - Multi-container orchestration

### Framework Examples

Laravel-specific configurations (adapt patterns for Symfony, WordPress, etc.):

- [Laravel Complete](laravel-complete) - Full Laravel stack with Horizon and queues
- [Laravel with Monitoring](laravel-with-monitoring) - Laravel + Prometheus + API

## Quick Start

### Minimal PHP-FPM

Works with any PHP application:

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

### PHP with Queue Workers

Example for Laravel (adapt for Symfony Messenger, WordPress, etc.):

```yaml
version: "1.0"

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  # Laravel: php artisan queue:work
  # Symfony: php bin/console messenger:consume
  # Custom: php worker.php
  queue-worker:
    enabled: true
    command: ["php", "artisan", "queue:work"]
    scale: 3
```

## See Also

- [Quickstart Guide](../getting-started/quickstart) - Step-by-step tutorial
- [Configuration Reference](../configuration/overview) - All options explained

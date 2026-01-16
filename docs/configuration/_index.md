---
title: "Configuration"
description: "Complete configuration reference for Cbox Init"
weight: 10
---

# Configuration

Complete reference for configuring Cbox Init with YAML and environment variables.

## In This Section

- [Configuration Overview](overview) - Complete YAML reference
- [Global Settings](global-settings) - Global configuration options
- [Process Configuration](processes) - Process-specific settings
- [Health Checks](health-checks) - Health check configuration
- [Lifecycle Hooks](lifecycle-hooks) - Pre/post start/stop hooks
- [Environment Variables](environment-variables) - ENV var reference
- [PHP-FPM Auto-Tuning](../php-fpm-autotune) - Intelligent worker configuration

## Quick Example

```yaml
version: "1.0"

global:
  shutdown_timeout: 30
  log_format: json
  log_level: info

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
```

See the [Quickstart Guide](../getting-started/quickstart) for a complete walkthrough.

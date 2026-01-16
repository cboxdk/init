---
title: "Configuration Overview"
description: "Complete YAML configuration reference for all Cbox Init settings and options"
weight: 11
---

# Configuration Overview

📝 **Documentation in Progress** - This comprehensive configuration reference is being written.

For now, please refer to:
- [Quickstart Guide](../getting-started/quickstart) - Basic configuration walkthrough
- [Example Configurations](https://github.com/cboxdk/init/tree/main/configs/examples) - Working YAML examples

## Coming Soon

This page will include:
- Complete YAML schema reference
- All configuration options with defaults
- Validation rules and constraints
- Best practices and patterns
- Common configuration scenarios

## Quick Reference

### Minimal Configuration

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

### See Also

- [Global Settings](global-settings) - Global configuration deep-dive
- [Process Configuration](processes) - Process-specific settings
- [Environment Variables](environment-variables) - Override via ENV

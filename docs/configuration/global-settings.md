---
title: "Global Settings"
description: "Global configuration options for shutdown timeout, logging, metrics, and API settings"
weight: 12
---

# Global Settings

Global settings apply to all processes and control system-wide behavior.

## Configuration Structure

```yaml
version: "1.0"

global:
  shutdown_timeout: 30
  log_format: json
  log_level: info
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: /metrics
  api_enabled: true
  api_port: 9180
  api_auth: "your-secure-token"
```

## Available Settings

### shutdown_timeout

**Type:** `integer` (seconds)
**Default:** `30`
**Description:** Maximum time to wait for all processes to stop gracefully before force-kill.

```yaml
global:
  shutdown_timeout: 60  # Wait up to 60 seconds
```

**When to adjust:**
- **Increase** (60-120s) for processes with long cleanup (database connections, file uploads)
- **Decrease** (10-20s) for fast-stopping processes (static file servers)

### log_format

**Type:** `string`
**Options:** `json`, `text`
**Default:** `json`
**Description:** Output format for structured logs.

```yaml
global:
  log_format: json  # Recommended for production
```

**Recommendations:**
- `json` - Production environments, log aggregation (Datadog, Splunk)
- `text` - Local development, human-readable output

### log_level

**Type:** `string`
**Options:** `debug`, `info`, `warn`, `error`
**Default:** `info`
**Description:** Minimum log level to output.

```yaml
global:
  log_level: info
```

**Level Guide:**
- `debug` - Verbose logging for development and troubleshooting
- `info` - Standard production logging (recommended)
- `warn` - Warnings and errors only
- `error` - Errors only (not recommended, may miss important warnings)

### Advanced Logging

#### Multiline Log Handling

```yaml
global:
  log_multiline_enabled: true
  log_multiline_pattern: '^\\[|^\\d{4}-|^{"'  # Regex for line starts
  log_multiline_timeout: 500  # milliseconds
  log_multiline_max_lines: 100
```

**Use cases:**
- PHP stack traces
- Multi-line error messages
- Formatted JSON logs

#### Sensitive Data Redaction

```yaml
global:
  log_redaction_enabled: true
  log_redaction_patterns:
    - "password"
    - "api_key"
    - "secret"
    - "token"
  log_redaction_placeholder: "***REDACTED***"
```

**Compliance:**
- GDPR: Automatic PII redaction
- PCI DSS: Credit card masking
- HIPAA: PHI protection patterns

#### Log Filtering

```yaml
global:
  log_filter_enabled: true
  log_filter_level: "info"  # Only INFO and above
  log_filter_patterns:
    - "health_check"  # Exclude health check logs
    - "metrics_export"  # Exclude metrics logs
```

### Restart Configuration

```yaml
global:
  restart_policy: always           # always | on-failure | never
  max_restart_attempts: 5          # 0 = unlimited
  restart_backoff_initial: 5s      # Initial delay before restart
  restart_backoff_max: 60s         # Maximum delay cap
```

- `restart_backoff_initial` and `restart_backoff_max` accept Go duration strings (`5s`, `1m`, etc.).
- Backoff grows exponentially (`initial * 2^n`) and is clamped by `restart_backoff_max`.
- Legacy `restart_backoff` (integer seconds) is still accepted for backwards compatibility.

### Metrics Configuration

```yaml
global:
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: /metrics
```

**Settings:**
- `metrics_enabled` - Enable/disable Prometheus metrics (default: `false`)
- `metrics_port` - HTTP port for metrics endpoint (default: `9090`)
- `metrics_path` - URL path for metrics (default: `/metrics`)

**Access metrics:**
```bash
curl http://localhost:9090/metrics
```

See [Prometheus Metrics](../observability/metrics) for complete metric documentation.

### Management API Configuration

```yaml
global:
  api_enabled: true
  api_port: 9180
  api_auth: "your-secure-token"
```

**Settings:**
- `api_enabled` - Enable/disable REST API (default: `true`)
- `api_port` - HTTP port for API endpoints (default: `9180`)
- `api_auth` - Optional Bearer token for authentication

**API Endpoints:**
- `GET /api/v1/health` - API health check
- `GET /api/v1/processes` - List processes
- `POST /api/v1/processes/{name}/restart` - Restart process

See [Management API](../observability/api) for complete API documentation.

> **Note:** The API is enabled by default to support the TUI and remote management. Set `api_enabled: false` (or `CBOX_INIT_GLOBAL_API_ENABLED=false`) to disable it entirely.

### Container Readiness Configuration

For Kubernetes integration, Cbox Init can manage a readiness file that indicates when all processes are healthy:

```yaml
global:
  readiness:
    enabled: true
    path: "/tmp/cbox-ready"
    mode: "all_healthy"
    processes:
      - php-fpm
      - nginx
```

**Settings:**
- `enabled` - Enable/disable readiness file management (default: `false`)
- `path` - Path to the readiness file (default: `/tmp/cbox-ready`)
- `mode` - Readiness evaluation mode: `all_healthy` or `all_running` (default: `all_healthy`)
- `content` - Custom file content when ready (optional)
- `processes` - Specific processes to track (empty = all processes)

**Kubernetes readinessProbe example:**
```yaml
readinessProbe:
  exec:
    command: ["test", "-f", "/tmp/cbox-ready"]
  initialDelaySeconds: 5
  periodSeconds: 5
```

See [Container Readiness](../features/container-readiness) for complete documentation.

## Environment Variable Overrides

All global settings can be overridden via environment variables:

```bash
# Override log level
CBOX_INIT_GLOBAL_LOG_LEVEL=debug ./cbox-init

# Override shutdown timeout
CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT=60 ./cbox-init

# Enable metrics
CBOX_INIT_GLOBAL_METRICS_ENABLED=true ./cbox-init
```

**Pattern:** `CBOX_INIT_GLOBAL_<SETTING_NAME>=<value>`

See [Environment Variables](environment-variables) for complete reference.

## Complete Example

```yaml
version: "1.0"

global:
  # Shutdown
  shutdown_timeout: 60

  # Logging
  log_format: json
  log_level: info
  log_multiline_enabled: true
  log_redaction_enabled: true
  log_redaction_patterns:
    - "password"
    - "token"

  # Observability
  metrics_enabled: true
  metrics_port: 9090

  api_enabled: true
  api_port: 9180
  api_auth: "your-secure-token-here"

  # Kubernetes Integration
  readiness:
    enabled: true
    path: "/tmp/cbox-ready"
    mode: "all_healthy"
    processes:
      - php-fpm
      - nginx

processes:
  # ... process configurations
```

## See Also

- [Process Configuration](processes) - Process-specific settings
- [Environment Variables](environment-variables) - ENV var reference
- [Container Readiness](../features/container-readiness) - K8s readiness probes
- [Prometheus Metrics](../observability/metrics) - Metrics documentation
- [Management API](../observability/api) - API documentation

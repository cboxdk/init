---
title: "Health Checks Configuration"
description: "Configure TCP, HTTP, and exec health checks with period, timeout, failure_threshold, and success_threshold"
weight: 14
---

# Health Checks Configuration

Configure health monitoring for your processes to ensure reliability and enable proper dependency management.

## Overview

Health checks monitor process health and enable:
- ✅ Automatic restart on failure (liveness)
- ✅ Dependency waiting (processes wait for healthy dependencies) (readiness)
- ✅ Health status reporting via metrics and API
- ✅ Graceful degradation patterns

## Basic Configuration

```yaml
processes:
  nginx:
    command: ["nginx", "-g", "daemon off;"]
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      period: 10
      timeout: 5
      failure_threshold: 3
      success_threshold: 2
```

## Health Check Types

### HTTP Health Check

```yaml
health_check:
  type: http
  url: "http://127.0.0.1:80/health"
  period: 10
  timeout: 5
  failure_threshold: 3
  success_threshold: 2
  expected_status: 200
```

**Settings:**
- `url` - Full HTTP URL to check (HTTP only)
- `expected_status` - Expected HTTP status code (default: `200`)

The check performs an HTTP `GET` and passes only when the response status code
equals `expected_status`. There is no response-body matching.

**Best for:**
- Web servers (Nginx, Apache)
- HTTP APIs
- Services with health endpoints

**Example Endpoint:**
```php
// Simple health endpoint (any PHP framework)
<?php
http_response_code(200);
header('Content-Type: application/json');
echo json_encode(['status' => 'healthy']);
```

### TCP Health Check

```yaml
health_check:
  type: tcp
  address: "127.0.0.1:9000"
  period: 10
  timeout: 3
  failure_threshold: 3
```

**Settings:**
- `address` - TCP address in format `host:port` (TCP only)

**Best for:**
- PHP-FPM (port 9000)
- Redis (port 6379)
- MySQL (port 3306)
- Services that listen on TCP ports

**Example:**
```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      period: 5
```

### Exec Health Check

```yaml
health_check:
  type: exec
  command: ["php", "artisan", "health:check"]
  period: 30
  timeout: 10
  failure_threshold: 2
```

**Settings:**
- `command` - Command to execute (array format, exec only)
- Process is healthy if exit code is `0`

**Best for:**
- Custom health logic
- Database connectivity checks
- Application-specific validation
- Multi-service health aggregation

**Example Health Check Script:**
```php
<?php
// health-check.php - works with any PHP framework/app

// Check database (PDO)
try {
    $pdo = new PDO('mysql:host=localhost;dbname=myapp', 'user', 'pass');
    $pdo->query('SELECT 1');
} catch (PDOException $e) {
    echo "Database connection failed\n";
    exit(1);
}

echo "All systems healthy\n";
exit(0);  // Success
```

## Common Settings

All health check types share these fields. Defaults are applied automatically.

| Field | Type | Default | Applies to | Description |
|-------|------|---------|------------|-------------|
| `type` | string | — (required) | all | `tcp`, `http`, or `exec` |
| `url` | string | — | http | Full HTTP URL to GET |
| `address` | string | — | tcp | `host:port` to dial |
| `command` | list | — | exec | Command + args; healthy on exit code 0 |
| `initial_delay` | int (s) | `5` | all | Grace period before the first check runs |
| `period` | int (s) | `10` | all | Time between checks |
| `timeout` | int (s) | `3` | all | Max wait per check |
| `failure_threshold` | int | `3` | all | Consecutive failures before unhealthy |
| `success_threshold` | int | `1` | all | Consecutive successes before healthy |
| `expected_status` | int | `200` | http | Required HTTP status code |
| `mode` | string | `both` | all | `liveness`, `readiness`, or `both` |

### initial_delay

**Type:** `integer` (seconds) · **Default:** `5`

Time to wait after the process starts before running the first health check.
Give slow-starting services enough time to bind their ports.

### period

**Type:** `integer` (seconds) · **Default:** `10`

Time between health checks.

```yaml
health_check:
  period: 30  # Check every 30 seconds
```

**Recommendations:**
- **Critical services:** 5-10 seconds
- **Standard services:** 10-30 seconds
- **Heavy checks:** 30-60 seconds

### timeout

**Type:** `integer` (seconds) · **Default:** `3`

Maximum time to wait for a health check response. Should be less than `period`.

### failure_threshold

**Type:** `integer` · **Default:** `3`

Number of consecutive failures before the process is marked unhealthy.

```yaml
health_check:
  failure_threshold: 5  # Allow 5 failures before marking unhealthy
```

### success_threshold

**Type:** `integer` · **Default:** `1`

Number of consecutive successes before the process is marked healthy. Useful
for services with a slow warm-up to prevent flapping.

### mode

**Type:** `string` · **Default:** `both`

Controls how the check result is used:

- `liveness` - failures trigger a restart (per the restart policy)
- `readiness` - result gates dependents (`depends_on`) but does not restart
- `both` - used for both liveness and readiness

## Health Check Lifecycle

```
[Process Starts]
      ↓
  [initial_delay] ← no checks yet
      ↓
  [Healthy] ← success_threshold consecutive successes
      ↓
  [Unhealthy] ← failure_threshold consecutive failures
      ↓
  [Restart] ← if mode includes liveness and restart policy allows
```

## Advanced Patterns

### Dependency Waiting (readiness)

```yaml
processes:
  php-fpm:
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      mode: both

  nginx:
    depends_on: [php-fpm]  # Waits for PHP-FPM readiness before starting
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      mode: both
```

### Multi-Layer Health Checks

```yaml
processes:
  app:
    command: ["./my-app"]
    health_check:
      type: exec
      command: ["/health-check.sh"]
      period: 30
```

**health-check.sh:**
```bash
#!/bin/bash
set -e

# Check HTTP endpoint
curl -f http://localhost:9180/health || exit 1

# Check database
psql -U user -d db -c "SELECT 1" || exit 1

echo "Health check passed"
exit 0
```

## Complete Examples

### PHP Application

```yaml
processes:
  # PHP-FPM with TCP check
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      period: 5
      timeout: 2
      failure_threshold: 3

  # Nginx with HTTP check
  nginx:
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      period: 10
      timeout: 5
      failure_threshold: 3
      expected_status: 200

  # Queue worker with exec check (Laravel example)
  queue-worker:
    command: ["php", "artisan", "queue:work"]
    health_check:
      type: exec
      command: ["pgrep", "-f", "queue:work"]
      period: 60
      timeout: 10
      failure_threshold: 2
```

### Microservices Stack

```yaml
processes:
  database:
    command: ["postgres"]
    health_check:
      type: tcp
      address: "127.0.0.1:5432"
      period: 5

  cache:
    command: ["redis-server"]
    health_check:
      type: tcp
      address: "127.0.0.1:6379"
      period: 5

  api:
    command: ["./api-server"]
    depends_on: [database, cache]
    health_check:
      type: http
      url: "http://127.0.0.1:8080/health"
      period: 10
      expected_status: 200

  worker:
    command: ["./worker"]
    depends_on: [database, cache]
    health_check:
      type: exec
      command: ["./worker-health-check"]
      period: 30
```

## Troubleshooting

### Health Check Always Failing

```yaml
# Increase timeout
health_check:
  timeout: 15  # Was 3, too short

# Allow more failures before flipping
health_check:
  failure_threshold: 5  # Was 3

# Check less frequently
health_check:
  period: 30  # Was 10
```

### Health Check Too Sensitive

```yaml
# Require sustained success
health_check:
  success_threshold: 5  # Was 1

# Tolerate transient failures
health_check:
  failure_threshold: 10  # Was 3
```

### Slow Startup Detection

```yaml
# Wait longer before checking, and require sustained success
health_check:
  initial_delay: 20   # Wait 20s before the first check
  success_threshold: 3
  period: 10
  timeout: 10
```

## Monitoring Health Status

### Via Metrics

```bash
# Prometheus metrics
curl http://localhost:9090/metrics | grep health

# Example output (labels: name, type)
cbox_init_health_check_status{name="nginx",type="http"} 1  # 1 = healthy
```

### Via Management API

```bash
# Get process status
curl http://localhost:9180/api/v1/processes | jq '.'
```

## See Also

- [Process Configuration](processes) - Process settings
- [Lifecycle Hooks](lifecycle-hooks) - Pre/post hooks
- [Prometheus Metrics](../observability/metrics) - Health metrics
- [Management API](../observability/api) - Health status API

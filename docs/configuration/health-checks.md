---
title: "Health Checks Configuration"
description: "Configure TCP, HTTP, and exec health checks with intervals, timeouts, retries, and success thresholds"
weight: 14
---

# Health Checks Configuration

Configure health monitoring for your processes to ensure reliability and enable proper dependency management.

## Overview

Health checks monitor process health and enable:
- ✅ Automatic restart on failure
- ✅ Dependency waiting (processes wait for healthy dependencies)
- ✅ Health status reporting via metrics and API
- ✅ Graceful degradation patterns

## Basic Configuration

```yaml
processes:
  nginx:
    command: ["nginx", "-g", "daemon off;"]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
      interval: 10
      timeout: 5
      retries: 3
      success_threshold: 2
```

## Health Check Types

### HTTP Health Check

```yaml
health_check:
  type: http
  address: "http://127.0.0.1:80/health"
  interval: 10
  timeout: 5
  retries: 3
  success_threshold: 2
  expected_status: 200
  expected_body: "OK"
```

**Settings:**
- `address` - Full HTTP URL to check
- `expected_status` - Expected HTTP status code (default: `200`)
- `expected_body` - Optional body content match

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
  interval: 10
  timeout: 3
  retries: 3
```

**Settings:**
- `address` - TCP address in format `host:port`

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
      interval: 5
```

### Exec Health Check

```yaml
health_check:
  type: exec
  command: ["php", "artisan", "health:check"]
  interval: 30
  timeout: 10
  retries: 2
```

**Settings:**
- `command` - Command to execute (array format)
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

// Check Redis (if using)
$redis = new Redis();
if (!$redis->connect('localhost')) {
    echo "Redis connection failed\n";
    exit(1);
}

echo "All systems healthy\n";
exit(0);  // Success
```

## Common Settings

### interval

**Type:** `integer` (seconds)
**Default:** `10`
**Description:** Time between health checks.

```yaml
health_check:
  interval: 30  # Check every 30 seconds
```

**Recommendations:**
- **Critical services:** 5-10 seconds
- **Standard services:** 10-30 seconds
- **Heavy checks:** 30-60 seconds
- **Exec commands:** 30-120 seconds (depending on execution time)

### timeout

**Type:** `integer` (seconds)
**Default:** `5`
**Description:** Maximum time to wait for health check response.

```yaml
health_check:
  timeout: 10  # Wait up to 10 seconds
```

**Guidelines:**
- Should be less than `interval`
- HTTP checks: 3-10 seconds
- TCP checks: 1-5 seconds
- Exec checks: 5-30 seconds (match command execution time)

### retries

**Type:** `integer`
**Default:** `3`
**Description:** Number of consecutive failures before marking unhealthy.

```yaml
health_check:
  retries: 5  # Allow 5 failures before marking unhealthy
```

**Recommendations:**
- **Stable services:** 2-3 retries
- **Flaky services:** 5-10 retries
- **Critical services:** 1-2 retries (fail fast)

### success_threshold

**Type:** `integer`
**Default:** `1`
**Description:** Number of consecutive successes before marking healthy.

```yaml
health_check:
  success_threshold: 3  # Require 3 successes to become healthy
```

**Use cases:**
- **Services with slow startup:** Require multiple successes (2-5)
- **Fast services:** Single success (1)
- **Flaky services:** Require sustained success (3-5)

## Health Check Lifecycle

```
[Process Starts]
      ↓
  [Starting] ← Health checks not yet running
      ↓
  [Healthy] ← success_threshold consecutive successes
      ↓
  [Unhealthy] ← retries consecutive failures
      ↓
  [Restart] ← If restart policy allows
```

**State Transitions:**
1. Process starts → `Starting` state
2. After `success_threshold` successes → `Healthy`
3. After `retries` failures → `Unhealthy`
4. Unhealthy process restarts (if `restart: always`)

## Advanced Patterns

### Dependency Waiting

```yaml
processes:
  php-fpm:
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    depends_on: [php-fpm]  # Waits for PHP-FPM to be healthy
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
```

**Behavior:**
- Nginx waits for PHP-FPM to reach `Healthy` state
- If PHP-FPM becomes unhealthy, Nginx continues running
- On restart, Nginx waits again for PHP-FPM

### Multi-Layer Health Checks

```yaml
processes:
  app:
    command: ["./my-app"]
    health_check:
      type: exec
      command: ["/health-check.sh"]
      interval: 30
```

**health-check.sh:**
```bash
#!/bin/bash
set -e

# Check HTTP endpoint
curl -f http://localhost:9180/health || exit 1

# Check database
psql -U user -d db -c "SELECT 1" || exit 1

# Check disk space
df -h / | awk 'NR==2 {if ($5+0 > 90) exit 1}'

echo "Health check passed"
exit 0
```

### Graceful Degradation

```yaml
processes:
  primary-service:
    command: ["./primary"]
    health_check:
      type: http
      address: "http://127.0.0.1:8080/health"
      retries: 10  # Tolerate more failures
      success_threshold: 3  # Require sustained success

  fallback-service:
    enabled: false  # Enable manually when primary fails
    command: ["./fallback"]
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
      interval: 5
      timeout: 2
      retries: 3

  # Nginx with HTTP check
  nginx:
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
      interval: 10
      timeout: 5
      retries: 3
      expected_status: 200

  # Queue worker with exec check (Laravel example)
  queue-worker:
    command: ["php", "artisan", "queue:work"]
    health_check:
      type: exec
      command: ["pgrep", "-f", "queue:work"]
      interval: 60
      timeout: 10
      retries: 2
```

### Microservices Stack

```yaml
processes:
  database:
    command: ["postgres"]
    health_check:
      type: tcp
      address: "127.0.0.1:5432"
      interval: 5

  cache:
    command: ["redis-server"]
    health_check:
      type: tcp
      address: "127.0.0.1:6379"
      interval: 5

  api:
    command: ["./api-server"]
    depends_on: [database, cache]
    health_check:
      type: http
      address: "http://127.0.0.1:8080/health"
      interval: 10
      expected_body: '{"status":"ok"}'

  worker:
    command: ["./worker"]
    depends_on: [database, cache]
    health_check:
      type: exec
      command: ["./worker-health-check"]
      interval: 30
```

## Troubleshooting

### Health Check Always Failing

```yaml
# Increase timeout
health_check:
  timeout: 15  # Was 5, too short

# Allow more retries
health_check:
  retries: 5  # Was 3

# Check less frequently
health_check:
  interval: 30  # Was 10
```

### Health Check Too Sensitive

```yaml
# Require sustained success
health_check:
  success_threshold: 5  # Was 1

# Tolerate transient failures
health_check:
  retries: 10  # Was 3
```

### Slow Startup Detection

```yaml
# Wait longer for initial health
health_check:
  success_threshold: 3  # Require 3 successes
  interval: 10
  timeout: 10
  # First success after 10s, healthy after 30s (3 × 10s)
```

## Monitoring Health Status

### Via Metrics

```bash
# Prometheus metrics
curl http://localhost:9090/metrics | grep health

# Example output
cbox_init_process_health_status{process="nginx"} 1  # 1 = healthy
```

### Via Management API

```bash
# Get process status
curl http://localhost:9180/api/v1/processes | jq '.[] | {name, health_status}'

# Example output
{
  "name": "nginx",
  "health_status": "healthy"
}
```

## See Also

- [Process Configuration](processes) - Process settings
- [Lifecycle Hooks](lifecycle-hooks) - Pre/post hooks
- [Prometheus Metrics](../observability/metrics) - Health metrics
- [Management API](../observability/api) - Health status API

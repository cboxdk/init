---
title: "Health Checks"
description: "Configure TCP, HTTP, and exec-based health monitoring with success thresholds and intelligent restart policies"
weight: 22
---

# Health Checks

Cbox Init provides comprehensive health monitoring with TCP, HTTP, and exec-based health checks. Health checks prevent restart loops, enable dependency verification, and support both readiness and liveness probes.

## Overview

**Features:**
- 🌐 **TCP Health Checks** - Port connectivity testing
- 📡 **HTTP Health Checks** - Endpoint validation with status codes
- ⚙️ **Exec Health Checks** - Custom command validation
- 🎯 **Success Thresholds** - Prevent flapping with consecutive success requirements
- 🔄 **Configurable Retries** - Automatic retry with timeouts
- 📊 **Prometheus Metrics** - Health check duration and status tracking

## Quick Start

```yaml
processes:
  nginx:
    enabled: true
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

### 1. TCP Health Check

**Tests port connectivity** - Useful for databases, caches, and services without HTTP endpoints.

```yaml
processes:
  redis:
    enabled: true
    command: ["redis-server", "/etc/redis/redis.conf"]
    health_check:
      type: tcp
      address: "127.0.0.1:6379"
      interval: 5
      timeout: 2
      retries: 3
      success_threshold: 1
```

**Configuration:**
- `type: tcp` - Required
- `address` - Format: `host:port` (e.g., `127.0.0.1:6379`, `localhost:3306`)
- Tests if port accepts connections

**Use cases:**
- MySQL: `127.0.0.1:3306`
- PostgreSQL: `127.0.0.1:5432`
- Redis: `127.0.0.1:6379`
- PHP-FPM: `127.0.0.1:9000`
- Memcached: `127.0.0.1:11211`

### 2. HTTP Health Check

**Tests HTTP endpoints** - Validates HTTP status codes and response bodies.

```yaml
processes:
  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
      interval: 10
      timeout: 5
      retries: 3
      success_threshold: 2
```

**Configuration:**
- `type: http` - Required
- `address` - Full URL (e.g., `http://localhost:80/health`)
- Expects: HTTP status 200-299 (success)
- Fails: HTTP status ≥300 or connection error

**Use cases:**
- Nginx health endpoint: `http://127.0.0.1:80/health`
- Application health check: `http://127.0.0.1:80/api/health`
- Custom health route: `http://127.0.0.1:8080/status`

**PHP health endpoint example:**
```php
// Simple health endpoint (works with any framework)
<?php
http_response_code(200);
header('Content-Type: application/json');
echo json_encode(['status' => 'healthy']);
```

### 3. Exec Health Check

**Runs custom commands** - Maximum flexibility for complex health validation.

```yaml
processes:
  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    health_check:
      type: exec
      command: ["php", "artisan", "horizon:status"]
      interval: 30
      timeout: 10
      retries: 2
      success_threshold: 1
```

**Configuration:**
- `type: exec` - Required
- `command` - Array of command and arguments
- Expects: Exit code 0 (success)
- Fails: Exit code ≠0 or timeout

**Use cases:**
- Process checks: `["pgrep", "-f", "queue:work"]`
- Horizon status: `["php", "artisan", "horizon:status"]`
- Database ping: `["mysql", "-u", "root", "-e", "SELECT 1"]`
- Custom scripts: `["./scripts/healthcheck.sh"]`

**Custom health check script:**
```bash
#!/bin/bash
# scripts/healthcheck.sh

# Check if process is running
if pgrep -f "queue:work" > /dev/null; then
  exit 0  # Healthy
else
  exit 1  # Unhealthy
fi
```

## Configuration Parameters

### Core Settings

```yaml
health_check:
  type: http                  # Required: tcp, http, exec
  address: "..."              # Required for tcp/http
  command: [...]              # Required for exec
  interval: 10                # Seconds between checks (default: 30)
  timeout: 5                  # Max wait time per check (default: 30)
  retries: 3                  # Failed attempts before unhealthy (default: 3)
  success_threshold: 2        # Consecutive successes to mark healthy (default: 1)
```

### Parameter Details

**`interval`** - Time between health checks
- Default: 30 seconds
- Range: 1-300 seconds
- **Recommendation**: 10-30s for most applications
- **Too low**: High overhead, noise
- **Too high**: Slow failure detection

**`timeout`** - Maximum wait time per check
- Default: 30 seconds
- Range: 1-60 seconds
- **Recommendation**: 5-10s for HTTP/TCP, 10-30s for exec
- **Too low**: False positives during load spikes
- **Too high**: Delayed failure detection

**`retries`** - Failed attempts before marking unhealthy
- Default: 3
- Range: 1-10
- **Recommendation**: 3-5 retries for transient failures
- **Higher**: More tolerance for temporary issues
- **Lower**: Faster reaction to real failures

**`success_threshold`** - Consecutive successes to mark healthy
- Default: 1
- Range: 1-10
- **Recommendation**: 1-2 for stable services, 2-3 for flaky services
- **Use case**: Prevent restart flapping after recovery

## Health Check Lifecycle

### State Machine

```
[Starting] → [Checking] → [Healthy] → [Checking] → ...
                 ↓            ↓
              [Unhealthy] ← [Failed]
                 ↓
              [Restart]
```

**States:**
1. **Starting** - Process just started, health checks not yet active
2. **Checking** - Health check in progress
3. **Healthy** - Check passed, process operational
4. **Failed** - Single check failed, retries remaining
5. **Unhealthy** - All retries exhausted, restart triggered

### Example Timeline

```
Time  | State       | Check Result | Retries | Action
------|-------------|--------------|---------|--------
0s    | Starting    | -            | -       | Process starts
10s   | Checking    | Success      | 0       | Mark healthy (threshold=1)
20s   | Healthy     | Success      | 0       | Continue
30s   | Healthy     | Failed       | 1/3     | Retry
35s   | Checking    | Failed       | 2/3     | Retry
40s   | Checking    | Failed       | 3/3     | Mark unhealthy
40s   | Unhealthy   | -            | -       | Trigger restart
```

## Success Threshold Pattern

**Prevents restart flapping** by requiring multiple consecutive successes after failure.

### Without Success Threshold (threshold=1)

```
Check: ✓ ✓ ✗ ✓ ✗ ✓ ✗ ✓ ...
State: H H U R U R U R ...  (Flapping - many restarts)
```

### With Success Threshold (threshold=3)

```
Check: ✓ ✓ ✗ ✓ ✓ ✓ ✗ ✓ ✓ ✓ ...
State: H H U ? ? H U ? ? H ...  (Stable - fewer restarts)
```

**Configuration:**
```yaml
health_check:
  type: http
  address: "http://127.0.0.1:80/health"
  retries: 3
  success_threshold: 3  # Require 3 consecutive successes
```

**Use cases:**
- Services with warm-up period
- Apps with transient initialization issues
- Prevent restart storms during load spikes

## Integration with Restart Policies

Health checks work with restart policies to control process lifecycle:

```yaml
processes:
  worker:
    enabled: true
    command: ["php", "artisan", "queue:work"]
    restart: on-failure     # Only restart on failures
    health_check:
      type: exec
      command: ["pgrep", "-f", "queue:work"]
      interval: 30
      retries: 3
```

**Restart policy behaviors:**
- `always` - Restart on health check failure
- `on-failure` - Restart on health check failure (same as always)
- `never` - Health checks still run, but no restart triggered

## Prometheus Metrics

Health check metrics exported when `metrics_enabled: true`:

```promql
# Health check status (1=healthy, 0=unhealthy)
cbox_init_health_check_status{process="nginx", type="http"}

# Health check duration in seconds
cbox_init_health_check_duration_seconds{process="nginx", type="http"}

# Total health check failures
cbox_init_health_check_failures_total{process="nginx", type="http"}
```

**Grafana alerts:**
```yaml
- alert: HealthCheckFailing
  expr: cbox_init_health_check_status == 0
  for: 5m
  annotations:
    summary: "Process {{$labels.process}} health check failing"

- alert: SlowHealthCheck
  expr: cbox_init_health_check_duration_seconds > 5
  for: 5m
  annotations:
    summary: "Slow health check for {{$labels.process}}"
```

## Best Practices

### 1. Match Check Type to Service

```yaml
# ✅ TCP for database
redis:
  health_check:
    type: tcp
    address: "127.0.0.1:6379"

# ✅ HTTP for web server
nginx:
  health_check:
    type: http
    address: "http://127.0.0.1:80/health"

# ✅ Exec for queue worker
worker:
  health_check:
    type: exec
    command: ["pgrep", "-f", "queue:work"]
```

### 2. Set Appropriate Timeouts

```yaml
# ✅ Fast services - low timeout
health_check:
  type: tcp
  address: "127.0.0.1:6379"
  timeout: 2  # Redis is fast

# ✅ Slow services - higher timeout
health_check:
  type: exec
  command: ["./complex-health-check.sh"]
  timeout: 30  # Complex checks need time
```

### 3. Use Success Thresholds for Flaky Services

```yaml
# ✅ Prevent restart flapping
health_check:
  type: http
  address: "http://127.0.0.1:80/health"
  success_threshold: 3  # Require 3 consecutive successes
```

### 4. Combine with Dependencies

```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]  # Wait for PHP-FPM
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
```

## Troubleshooting

### Health Checks Always Failing

**Issue:** Process keeps restarting due to failed health checks

**Solutions:**

1. **Verify endpoint is reachable:**
   ```bash
   # HTTP
   curl -v http://127.0.0.1:80/health

   # TCP
   telnet 127.0.0.1 6379

   # Exec
   ./healthcheck.sh && echo "Success" || echo "Failed"
   ```

2. **Increase timeout:**
   ```yaml
   health_check:
     timeout: 10  # Increase from 5
   ```

3. **Check process startup time:**
   ```yaml
   health_check:
     interval: 30  # Give more time to start
     retries: 5    # More tolerance
   ```

4. **Add success threshold:**
   ```yaml
   health_check:
     success_threshold: 2  # Require 2 successes
   ```

### Process Not Restarting on Failure

**Issue:** Health check fails but process doesn't restart

**Solutions:**

1. **Check restart policy:**
   ```yaml
   restart: always  # Or on-failure
   ```

2. **Verify health check is configured:**
   ```yaml
   health_check:
     type: tcp
     address: "..."  # Must be present
   ```

3. **Check logs:**
   ```bash
   # View Cbox logs
   journalctl -u cbox-init -f | grep "health"
   ```

### False Positives During Load

**Issue:** Health checks fail during high load

**Solutions:**

1. **Increase timeout:**
   ```yaml
   health_check:
     timeout: 10  # More tolerance for load
   ```

2. **Reduce check frequency:**
   ```yaml
   health_check:
     interval: 60  # Less frequent checks
   ```

3. **Add success threshold:**
   ```yaml
   health_check:
     success_threshold: 3  # Prevent flapping
   ```

## Examples

### PHP Application

```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      interval: 10
      timeout: 5

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
      interval: 10
      timeout: 5
      retries: 3
      success_threshold: 2

  # Laravel Horizon example
  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    health_check:
      type: exec
      command: ["php", "artisan", "horizon:status"]
      interval: 30
      timeout: 10
      retries: 2
```

### Database Services

```yaml
processes:
  redis:
    enabled: true
    command: ["redis-server"]
    health_check:
      type: tcp
      address: "127.0.0.1:6379"
      interval: 5
      timeout: 2

  mysql:
    enabled: true
    command: ["mysqld"]
    health_check:
      type: tcp
      address: "127.0.0.1:3306"
      interval: 10
      timeout: 5
```

### Queue Workers

```yaml
processes:
  queue-default:
    enabled: true
    command: ["php", "artisan", "queue:work", "--tries=3"]
    scale: 3
    health_check:
      type: exec
      command: ["pgrep", "-f", "queue:work"]
      interval: 30
      timeout: 5
      retries: 3
      success_threshold: 1
```

## Next Steps

- [Configuration Reference](../configuration/health-checks) - Complete configuration options
- [Restart Policies](restart-policies) - Process restart strategies
- [Prometheus Metrics](../observability/metrics) - Metrics and monitoring
- [Examples](../examples) - Real-world configurations

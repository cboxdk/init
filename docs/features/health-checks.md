---
title: "Health Checks"
description: "Configure TCP, HTTP, and exec-based health monitoring with success/failure thresholds and liveness/readiness modes"
weight: 22
---

# Health Checks

Cbox Init provides health monitoring with TCP, HTTP, and exec-based health checks. Health checks prevent restart loops, enable dependency verification, and support both readiness and liveness modes.

## Overview

**Features:**
- 🌐 **TCP Health Checks** - Port connectivity testing
- 📡 **HTTP Health Checks** - Endpoint validation with status codes
- ⚙️ **Exec Health Checks** - Custom command validation
- 🎯 **Success Thresholds** - Prevent flapping with consecutive-success requirements
- 🔄 **Failure Thresholds** - Configurable tolerance before marking unhealthy
- 📊 **Prometheus Metrics** - Health check duration and status tracking

## Quick Start

```yaml
processes:
  nginx:
    enabled: true
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
      period: 5
      timeout: 2
      failure_threshold: 3
      success_threshold: 1
```

**Configuration:**
- `type: tcp` - Required
- `address` - Format: `host:port` (e.g., `127.0.0.1:6379`, `localhost:3306`)
- Tests if the port accepts connections

**Use cases:** MySQL `127.0.0.1:3306`, PostgreSQL `127.0.0.1:5432`, Redis `127.0.0.1:6379`, PHP-FPM `127.0.0.1:9000`, Memcached `127.0.0.1:11211`.

### 2. HTTP Health Check

**Tests HTTP endpoints** - Validates the HTTP status code.

```yaml
processes:
  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      period: 10
      timeout: 5
      failure_threshold: 3
      success_threshold: 2
      expected_status: 200
```

**Configuration:**
- `type: http` - Required
- `url` - Full URL (e.g., `http://localhost:80/health`)
- `expected_status` - Required status code (default `200`)
- The check performs a `GET` and passes only when the status equals
  `expected_status`. There is no response-body matching.

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
      period: 30
      timeout: 10
      failure_threshold: 2
      success_threshold: 1
```

**Configuration:**
- `type: exec` - Required
- `command` - Array of command and arguments
- Passes on exit code 0; fails on non-zero exit or timeout

## Configuration Parameters

```yaml
health_check:
  type: http                  # Required: tcp | http | exec
  url: "..."                  # Required for http
  address: "..."              # Required for tcp (host:port)
  command: [...]              # Required for exec
  initial_delay: 5            # Seconds before the first check (default: 5)
  period: 10                  # Seconds between checks (default: 10)
  timeout: 3                  # Max wait per check (default: 3)
  failure_threshold: 3        # Consecutive failures before unhealthy (default: 3)
  success_threshold: 1        # Consecutive successes before healthy (default: 1)
  expected_status: 200        # HTTP only (default: 200)
  mode: both                  # liveness | readiness | both (default: both)
```

### Parameter Details

**`initial_delay`** - Grace period after start before the first check runs. Default 5s.

**`period`** - Time between checks. Default 10s. Recommendation: 10-30s for most applications.

**`timeout`** - Maximum wait per check. Default 3s. Should be less than `period`.

**`failure_threshold`** - Consecutive failures before marking unhealthy. Default 3.

**`success_threshold`** - Consecutive successes before marking healthy. Default 1. Use 2-3 for flaky services to prevent flapping.

**`mode`** - How the result is used:
- `liveness` - failures trigger a restart (per the restart policy)
- `readiness` - result gates dependents (`depends_on`) but does not restart
- `both` (default) - used for both

## Success Threshold Pattern

**Prevents restart flapping** by requiring multiple consecutive successes after a failure.

```yaml
health_check:
  type: http
  url: "http://127.0.0.1:80/health"
  failure_threshold: 3
  success_threshold: 3  # Require 3 consecutive successes to be healthy again
```

## Integration with Restart Policies

Health checks with a liveness mode work with restart policies to control process lifecycle:

```yaml
processes:
  worker:
    enabled: true
    command: ["php", "artisan", "queue:work"]
    restart: on-failure     # Only restart on failures
    health_check:
      type: exec
      command: ["pgrep", "-f", "queue:work"]
      period: 30
      failure_threshold: 3
      mode: liveness
```

**Restart policy behaviors:**
- `always` - The unhealthy instance is killed and restarted
- `on-failure` - The unhealthy instance is killed and restarted (the kill counts
  as a failure)
- `never` - Health checks run and report, but the process is left alone. Killing
  it would end the workload for the life of the container, since nothing would
  bring it back.

Note that a liveness kill is a hard `SIGKILL` to the process group: it does not
run the process's `pre_stop_hook` and does not use its configured
`shutdown.signal`. A queue worker killed this way loses whatever job it was
holding. If your workload needs a graceful drain, prefer `mode: readiness` and
let the process be taken out of rotation rather than killed.

## Prometheus Metrics

Health check metrics are exported when `metrics_enabled: true`. Labels are
`name` (the process) and `type` (the check type):

```promql
# Health check status (1=healthy, 0=unhealthy)
cbox_init_health_check_status{name="nginx", type="http"}

# Health check duration in seconds (histogram)
cbox_init_health_check_duration_seconds{name="nginx", type="http"}

# Total checks performed, by result (status="success"|"failure")
cbox_init_health_check_total{name="nginx", type="http", status="failure"}

# Current consecutive failures (gauge; label: name)
cbox_init_health_check_consecutive_fails{name="nginx"}
```

**Grafana alerts:**
```yaml
- alert: HealthCheckFailing
  expr: cbox_init_health_check_status == 0
  for: 5m
  annotations:
    summary: "Process {{$labels.name}} health check failing"

- alert: SlowHealthCheck
  expr: cbox_init_health_check_duration_seconds > 5
  for: 5m
  annotations:
    summary: "Slow health check for {{$labels.name}}"
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
    url: "http://127.0.0.1:80/health"

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
  timeout: 2

# ✅ Slow services - higher timeout
health_check:
  type: exec
  command: ["./complex-health-check.sh"]
  timeout: 30
```

### 3. Use Success Thresholds for Flaky Services

```yaml
health_check:
  type: http
  url: "http://127.0.0.1:80/health"
  success_threshold: 3
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
    depends_on: [php-fpm]  # Waits for PHP-FPM readiness
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
```

## Troubleshooting

### Health Checks Always Failing

1. **Verify endpoint is reachable:**
   ```bash
   curl -v http://127.0.0.1:80/health   # HTTP
   nc -z 127.0.0.1 6379                 # TCP
   ./healthcheck.sh && echo ok || echo fail  # Exec
   ```
2. **Increase timeout:** `timeout: 10`
3. **Give the process more time to start:** `initial_delay: 20`, `failure_threshold: 5`
4. **Add a success threshold:** `success_threshold: 2`

### Process Not Restarting on Failure

1. Check the restart policy: `restart: always` (or `on-failure`).
2. Ensure the health check has a liveness mode: `mode: both` or `mode: liveness`.
3. Check logs: `journalctl -u cbox-init -f | grep health`.

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
      period: 10
      timeout: 5

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      period: 10
      timeout: 5
      failure_threshold: 3
      success_threshold: 2

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    health_check:
      type: exec
      command: ["php", "artisan", "horizon:status"]
      period: 30
      timeout: 10
      failure_threshold: 2
```

## Next Steps

- [Configuration Reference](../configuration/health-checks) - Complete configuration options
- [Restart Policies](restart-policies) - Process restart strategies
- [Prometheus Metrics](../observability/metrics) - Metrics and monitoring

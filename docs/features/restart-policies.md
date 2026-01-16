---
title: "Restart Policies"
description: "Configure automatic restart behavior with always, on-failure, and never policies with exponential backoff"
weight: 24
---

# Restart Policies

Control how Cbox Init handles process exits with configurable restart policies and exponential backoff.

## Overview

Restart policies determine:
- ✅ **When to restart:** Based on exit code and policy
- ✅ **How often to retry:** Exponential backoff prevents restart loops
- ✅ **Resource protection:** Avoid infinite restart cycles
- ✅ **Self-healing:** Automatic recovery from transient failures
- ✅ **Intentional exits:** Respect clean shutdowns

## Available Policies

### always (Default)

**Restart the process regardless of exit code.**

```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    restart: always
```

**Behavior:**
- Exit code 0 (success) → Restart
- Exit code 1-255 (error) → Restart
- Crash/signal → Restart
- Manual stop → Restart

**Use cases:**
- Long-running services (PHP-FPM, Nginx, Horizon)
- Critical infrastructure
- Services that should never stop

### on-failure

**Restart only if process exits with non-zero code.**

```yaml
processes:
  queue-worker:
    command: ["php", "artisan", "queue:work"]
    restart: on-failure
```

**Behavior:**
- Exit code 0 (success) → Do NOT restart
- Exit code 1-255 (error) → Restart
- Crash/signal → Restart
- Manual stop → Do NOT restart

**Use cases:**
- Queue workers that can stop cleanly
- Batch processors
- Services with intentional shutdowns
- Development/testing

### never

**Never automatically restart the process.**

```yaml
processes:
  migration:
    command: ["php", "artisan", "migrate", "--force"]
    restart: never
```

**Behavior:**
- Exit code 0 → Stop, mark complete
- Exit code 1-255 → Stop, mark failed
- No automatic recovery

**Use cases:**
- One-time tasks (migrations, seed data)
- Scheduled tasks
- Manual intervention required
- Initialization scripts

## Exit Code Handling

### Exit Code Meanings

| Exit Code | Meaning | always | on-failure | never |
|-----------|---------|---------|------------|-------|
| 0 | Success | Restart | Stop | Stop |
| 1-127 | Application error | Restart | Restart | Stop |
| 128+N | Killed by signal N | Restart | Restart | Stop |
| 137 | SIGKILL (OOM) | Restart | Restart | Stop |
| 139 | SIGSEGV (segfault) | Restart | Restart | Stop |
| 143 | SIGTERM (graceful) | Restart | Restart | Stop |

### Common Exit Codes

**Exit 0 - Clean Shutdown:**
```php
// Laravel Command
public function handle()
{
    $this->info('Task completed successfully');
    return 0;  // Clean exit, no restart (if on-failure)
}
```

**Exit 1 - General Error:**
```php
public function handle()
{
    if (!$this->validateInput()) {
        $this->error('Invalid input');
        return 1;  // Error, will restart (if on-failure or always)
    }
}
```

**Exit 137 - OOM Killed:**
```bash
# Container ran out of memory
# Process was killed by kernel with SIGKILL
# Will restart automatically (if on-failure or always)
```

## Exponential Backoff

### Default Backoff

Cbox Init uses exponential backoff to prevent restart loops:

```
Attempt 1: Immediate restart
Attempt 2: Wait 1 second
Attempt 3: Wait 2 seconds
Attempt 4: Wait 4 seconds
Attempt 5: Wait 8 seconds
Attempt 6: Wait 16 seconds
...
Max wait: 60 seconds
```

### Configuration

```yaml
processes:
  unstable-service:
    command: ["./flaky-app"]
    restart: always
    restart_delay: 5  # Initial delay (seconds)
    max_restart_delay: 300  # Maximum delay (5 minutes)
```

**Behavior:**
```
Crash 1: Wait 5s, restart
Crash 2: Wait 10s, restart
Crash 3: Wait 20s, restart
Crash 4: Wait 40s, restart
Crash 5: Wait 80s, restart
Crash 6: Wait 160s, restart
Crash 7+: Wait 300s (max), restart
```

## Complete Examples

### Long-Running Services

```yaml
processes:
  # Always restart critical services
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    restart: always
    restart_delay: 1
    max_restart_delay: 60

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    restart: always
    restart_delay: 1

  horizon:
    command: ["php", "artisan", "horizon"]
    restart: always
    restart_delay: 5
    max_restart_delay: 300  # Allow longer backoff for horizon
```

### Queue Workers

```yaml
processes:
  # Restart on failure only
  queue-default:
    command: ["php", "artisan", "queue:work", "--tries=3"]
    restart: on-failure
    restart_delay: 2
```

**Why on-failure:**
- If worker exits cleanly (exit 0), it stays stopped
- Allows graceful scaling down
- Respects manual stops via API

### One-Time Tasks

```yaml
processes:
  # Run once, never restart
  migration:
    command: ["php", "artisan", "migrate", "--force"]
    restart: never

  seed-data:
    command: ["php", "artisan", "db:seed", "--class=ProductionSeeder"]
    restart: never
```

### Scheduled Tasks

```yaml
processes:
  # Cron handles scheduling, never auto-restart
  backup-daily:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"
    restart: never  # Required for scheduled tasks
```

## Restart Loop Prevention

### Maximum Restarts

```yaml
processes:
  problematic-service:
    command: ["./unstable-app"]
    restart: always
    max_restarts: 10  # Stop after 10 restarts
```

**Behavior:**
- After 10 restarts, process enters "failed" state
- No more automatic restarts
- Requires manual intervention

### Backoff Threshold

```yaml
processes:
  flaky-service:
    command: ["./flaky-app"]
    restart: always
    restart_delay: 10
    max_restart_delay: 600  # 10 minutes max
    restart_threshold: 60  # If restarts within 60s, increase backoff
```

**Behavior:**
- If process runs > 60 seconds, backoff resets
- If process crashes < 60 seconds, backoff increases

## Metrics and Monitoring

### Restart Count Metrics

```bash
# Total restarts per process
cbox_init_process_restarts_total{process="php-fpm"}

# Restart rate (restarts per second)
rate(cbox_init_process_restarts_total{process="php-fpm"}[5m])
```

### Alert on Excessive Restarts

```yaml
# Prometheus alert
groups:
  - name: restart_policies
    rules:
      - alert: FrequentRestarts
        expr: rate(cbox_init_process_restarts_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Process {{ $labels.process }} restarting frequently"

      - alert: RestartLoop
        expr: cbox_init_process_restarts_total > 10
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Process {{ $labels.process }} in restart loop"
```

## Troubleshooting

### Process Keeps Restarting

**Check logs for exit reason:**
```bash
docker logs app | grep "exited"

# Look for exit codes
# {"level":"ERROR","msg":"Process exited","process":"php-fpm","exit_code":137}
```

**Common exit codes:**
- **137 (SIGKILL):** OOM killed → Increase memory or reduce workers
- **139 (SIGSEGV):** Segfault → Check PHP extensions or code bugs
- **1:** Configuration error → Check process logs

**Solution for OOM:**
```yaml
# Reduce PHP-FPM workers
env:
  PHP_FPM_AUTOTUNE_PROFILE: light  # Was medium

# Or increase container memory
deploy:
  resources:
    limits:
      memory: 4G  # Was 2G
```

### Restart Loop

**Symptom:** Process restarts immediately, continuously

**Debug:**
```bash
# Watch restarts in real-time
docker logs -f app | grep restart
```

**Solutions:**
```yaml
# Option 1: Increase restart delay
processes:
  unstable-app:
    restart_delay: 30  # Wait 30s before retry

# Option 2: Change policy
processes:
  unstable-app:
    restart: on-failure  # Was always

# Option 3: Add max restarts
processes:
  unstable-app:
    max_restarts: 5  # Stop after 5 attempts
```

### Process Won't Restart

**Symptom:** Process stops but doesn't restart

**Check policy:**
```yaml
processes:
  my-app:
    restart: never  # ← This prevents restart
```

**Check exit code:**
```bash
# If policy is on-failure, exit 0 won't restart
docker logs app | grep "exit_code"
```

**Solution:**
```yaml
my-app:
  restart: always  # Change to always if needed
```

## Best Practices

### ✅ Do

**Match policy to process type:**
```yaml
# Long-running services
php-fpm:
  restart: always

# Queue workers
queue-worker:
  restart: on-failure

# One-time tasks
migration:
  restart: never
```

**Use backoff for unstable services:**
```yaml
flaky-service:
  restart: always
  restart_delay: 10
  max_restart_delay: 300
```

**Monitor restart rates:**
```promql
# Alert if restart rate too high
rate(cbox_init_process_restarts_total[5m]) > 0.1
```

**Set max restarts for safety:**
```yaml
experimental-service:
  restart: always
  max_restarts: 20  # Prevent infinite loops
```

### ❌ Don't

**Don't use always for scheduled tasks:**
```yaml
# ❌ Bad - will run continuously
backup-job:
  schedule: "0 2 * * *"
  restart: always  # Wrong!

# ✅ Good
backup-job:
  schedule: "0 2 * * *"
  restart: never
```

**Don't use never for critical services:**
```yaml
# ❌ Bad - no recovery from failures
php-fpm:
  restart: never  # Service won't recover!

# ✅ Good
php-fpm:
  restart: always
```

**Don't ignore restart loops:**
```bash
# ❌ Bad - process crashes every 2 seconds
# Do nothing and let it loop

# ✅ Good - investigate and fix root cause
docker logs app | grep "exit_code"
```

## See Also

- [Process Configuration](../configuration/processes) - Restart configuration
- [Health Checks](health-checks) - Health-based restart triggers
- [Prometheus Metrics](../observability/metrics) - Restart metrics
- [Management API](../observability/api) - Manual restart control

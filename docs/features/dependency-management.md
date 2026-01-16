---
title: "Dependency Management"
description: "DAG-based process startup ordering with topological sort and dependency waiting"
weight: 20
---

# Dependency Management

Cbox Init uses a Directed Acyclic Graph (DAG) to manage process dependencies and ensure correct startup ordering.

## Overview

Dependency management provides:
- ✅ **Correct startup order:** Processes start only after dependencies are healthy
- ✅ **Race condition prevention:** No "connection refused" errors from premature starts
- ✅ **Topological sorting:** Automatic dependency resolution
- ✅ **Circular dependency detection:** Validates configuration on startup
- ✅ **Health-aware waiting:** Waits for dependencies to pass health checks

## Basic Usage

```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]  # Wait for PHP-FPM

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    depends_on: [php-fpm, nginx]  # Wait for both
```

**Startup flow:**
```
PHP-FPM starts
    ↓
PHP-FPM passes health checks
    ↓
Nginx starts (dependency met)
    ↓
Nginx passes health checks
    ↓
Horizon starts (all dependencies met)
```

## How It Works

### Without Dependencies

```yaml
# Simple priority-based ordering
processes:
  php-fpm:

  nginx:

  worker:
```

**Behavior:** Processes start in priority order, but don't wait for each other.

**Problem:** Nginx might start before PHP-FPM is ready → 502 errors

### With Dependencies

```yaml
processes:
  php-fpm:
    health_check:
      type: tcp
      address: 127.0.0.1:9000

  nginx:
    depends_on: [php-fpm]  # WAIT for PHP-FPM health

  worker:
    depends_on: [php-fpm]  # WAIT for PHP-FPM health
```

**Behavior:** Dependent processes wait for dependencies to become healthy.

**Solution:** Nginx waits for PHP-FPM health check → No 502 errors

## Dependency Graph

### Simple Linear Dependency

```yaml
processes:
  database:

  app:
    depends_on: [database]

  worker:
    depends_on: [app]
```

**DAG:**
```
database → app → worker
```

### Multi-Dependency (Diamond Pattern)

```yaml
processes:
  database:

  cache:

  app:
    depends_on: [database, cache]

  worker:
    depends_on: [app]
```

**DAG:**
```
    database ──┐
               ├──> app ──> worker
    cache ─────┘
```

**Startup order:**
1. database + cache (parallel)
2. app (waits for both)
3. worker (waits for app)

### Complex Dependencies

```yaml
processes:
  postgres:

  redis:

  php-fpm:
    depends_on: [postgres, redis]

  nginx:
    depends_on: [php-fpm]

  horizon:
    depends_on: [php-fpm, redis]

  queue-default:
    depends_on: [php-fpm, redis]

  queue-high:
    depends_on: [php-fpm, redis]
```

**DAG:**
```
postgres ──┐
           ├──> php-fpm ──┐
redis ─────┤              ├──> nginx
           │              │
           ├──────────────┼──> horizon
           │              │
           └──────────────┴──> queue-high ──> queue-default
```

**Topological sort produces:**
1. postgres, redis (parallel, priority 10)
2. php-fpm (priority 20, waits for postgres + redis)
3. nginx (priority 30, waits for php-fpm)
4. queue-high (priority 45, waits for php-fpm + redis)
5. horizon (priority 40, waits for php-fpm + redis)
6. queue-default (priority 50, waits for php-fpm + redis)

## Health Check Integration

### With Health Checks

```yaml
processes:
  php-fpm:
    health_check:
      type: tcp
      address: 127.0.0.1:9000
      success_threshold: 2  # Require 2 successes

  nginx:
    depends_on: [php-fpm]
```

**Behavior:**
1. PHP-FPM starts
2. Health checks begin
3. After 2 successful checks → PHP-FPM is "healthy"
4. Nginx starts (dependency met)

### Without Health Checks

```yaml
processes:
  php-fpm:
    # No health check

  nginx:
    depends_on: [php-fpm]
```

**Behavior:**
1. PHP-FPM starts
2. Nginx starts **immediately** (no waiting)

**Warning:** Without health checks, `depends_on` only enforces startup order, not readiness!

## Configuration Validation

### Circular Dependency Detection

```yaml
# ❌ This will fail validation
processes:
  service-a:
    depends_on: [service-b]

  service-b:
    depends_on: [service-a]
```

**Error:**
```
Configuration validation failed: circular dependency detected: service-a → service-b → service-a
```

### Self-Dependency Detection

```yaml
# ❌ This will fail validation
processes:
  app:
    depends_on: [app]
```

**Error:**
```
Configuration validation failed: process 'app' cannot depend on itself
```

### Missing Dependency Detection

```yaml
# ❌ This will fail validation
processes:
  app:
    depends_on: [database]  # 'database' process not defined
```

**Error:**
```
Configuration validation failed: process 'app' depends on undefined process 'database'
```

## Advanced Patterns

### Optional Dependencies

```yaml
processes:
  app:
    enabled: true
    depends_on: [database, cache]

  database:
    enabled: true

  cache:
    enabled: false  # Optional service
```

**Behavior:**
- If cache is disabled, dependency is ignored
- App only waits for database

### Conditional Dependencies

```bash
# Use environment variables to control dependencies
export CBOX_INIT_PROCESS_CACHE_ENABLED=${ENABLE_CACHE:-false}
```

```yaml
processes:
  app:
    depends_on: [database, cache]

  cache:
    enabled: ${ENABLE_CACHE}  # Controlled via env var
```

### Multi-Stage Dependencies

```yaml
processes:
  # Stage 1: Infrastructure
  postgres:

  redis:

  # Stage 2: Application
  php-fpm:
    depends_on: [postgres, redis]

  # Stage 3: Web layer
  nginx:
    depends_on: [php-fpm]

  # Stage 4: Background services
  horizon:
    depends_on: [php-fpm, redis]

  queue:
    depends_on: [php-fpm, redis]
```

## Priority vs depends_on

### When to Use Priority

**Use priority for:**
- Simple ordering without health waiting
- Independent processes
- Performance optimization (parallel starts)

```yaml
processes:
  logger:

  app:

  metrics:
```

### When to Use depends_on

**Use depends_on for:**
- Health-dependent ordering
- Service readiness requirements
- Preventing connection errors

```yaml
processes:
  database:
    health_check:
      type: tcp
      address: 127.0.0.1:5432

  app:
    depends_on: [database]  # Wait for database health
```

### Combined Usage

```yaml
processes:
  # Infrastructure (priority 10)
  postgres:

  redis:

  # Application depends on infrastructure (priority 20)
  php-fpm:
    depends_on: [postgres, redis]

  # Web depends on application (priority 30)
  nginx:
    depends_on: [php-fpm]
```

**Effect:**
- Priority determines order within dependency level
- depends_on enforces health waiting between levels

## Dependency Timeout

### Default Timeout

```yaml
global:
  dependency_timeout: 300  # Wait up to 5 minutes for dependencies
```

### Per-Process Timeout

```yaml
processes:
  app:
    depends_on: [slow-service]
    dependency_timeout: 600  # Wait up to 10 minutes
```

## Troubleshooting

### Dependency Never Becomes Healthy

**Symptom:** Process never starts, waiting for dependency

**Debug:**
```bash
# Check logs
docker logs app | grep "waiting for dependency"

# Check dependency health
curl http://localhost:9180/api/v1/processes | jq '.[] | {name, health_status}'
```

**Solutions:**
```yaml
# Option 1: Increase health check retries
dependency:
  health_check:
    failure_threshold: 10  # Was 3

# Option 2: Increase success threshold delay
dependency:
  health_check:
    initial_delay: 30  # Was 10

# Option 3: Remove dependency if not critical
app:
  depends_on: []  # Remove dependency
```

### Circular Dependency

**Symptom:** Configuration validation fails

**Error:**
```
circular dependency detected: app → worker → app
```

**Solution:** Remove circular dependency
```yaml
# ❌ Bad
app:
  depends_on: [worker]
worker:
  depends_on: [app]

# ✅ Good
app:
  depends_on: [database]
worker:
  depends_on: [app]
```

### Long Startup Time

**Symptom:** Container takes minutes to start

**Debug:**
```bash
# Check which process is slow
docker logs app | grep "started successfully"
```

**Solutions:**
```yaml
# Option 1: Reduce health check intervals
dependency:
  health_check:
    period: 5  # Check more frequently

# Option 2: Reduce success threshold
dependency:
  health_check:
    success_threshold: 1  # Was 2

# Option 3: Start processes in parallel (remove depends_on)
app:
  depends_on: []  # Remove if not critical
```

## Best Practices

### ✅ Do

**Always use health checks with depends_on:**
```yaml
database:
  health_check:
    type: tcp  # Required for depends_on to wait
    address: 127.0.0.1:5432

app:
  depends_on: [database]
```

**Use meaningful process names:**
```yaml
# ✅ Good
depends_on: [postgres, redis, php-fpm]

# ❌ Avoid
depends_on: [proc1, service-x, thing]
```

**Document complex dependencies:**
```yaml
# Complex dependency graph - see architecture docs
api-gateway:
  depends_on: [auth-service, user-service, billing-service]
```

### ❌ Don't

**Don't create circular dependencies:**
```yaml
# ❌ Invalid
service-a:
  depends_on: [service-b]
service-b:
  depends_on: [service-a]
```

**Don't depend on disabled processes:**
```yaml
# ❌ Will fail
app:
  depends_on: [cache]

cache:
  enabled: false
```

**Don't over-specify dependencies:**
```yaml
# ❌ Over-specified
app:
  depends_on: [db, cache, logger, metrics, api, worker]

# ✅ Minimal
app:
  depends_on: [db, cache]  # Only critical dependencies
```

## See Also

- [Process Configuration](../configuration/processes) - Priority and depends_on settings
- [Health Checks](health-checks) - Health check configuration
- [Examples](../examples/) - Real-world dependency patterns

---
title: "Lifecycle Hooks"
description: "Configure pre/post start/stop hooks for PHP application optimization, migrations, and graceful shutdown"
weight: 15
---

# Lifecycle Hooks

Execute commands at specific points in the process lifecycle for setup, cleanup, and graceful shutdown.

## Overview

Lifecycle hooks enable:
- ✅ Database migrations before startup
- ✅ Cache warming and optimization
- ✅ Graceful service termination
- ✅ Cleanup operations on shutdown
- ✅ Service-specific preparation

## Hook Types

### Global Hooks

Run once for all processes:

```yaml
hooks:
  pre-start:
    - name: migrate-database
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

    - name: optimize-cache
      command: ["php", "artisan", "optimize"]
      timeout: 60

  post-start:
    - name: warmup-cache
      command: ["php", "artisan", "cache:warmup"]
      timeout: 120
```

### Process-Specific Hooks

Run for individual processes:

```yaml
processes:
  horizon:
    command: ["php", "artisan", "horizon"]
    shutdown:
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60
```

## Global Pre-Start Hooks

Execute **before** any processes start.

```yaml
hooks:
  pre-start:
    - name: wait-for-database
      command: ["./wait-for-db.sh"]
      timeout: 60

    - name: run-migrations
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

    - name: seed-data
      command: ["php", "artisan", "db:seed", "--class=ProductionSeeder"]
      timeout: 120
```

**Settings:**
- `name` - Hook identifier (for logging)
- `command` - Command to execute (array format)
- `timeout` - Maximum execution time in seconds

**Use Cases:**
- **Database migrations:** Run schema updates
- **Cache warming:** Pre-populate caches
- **Service dependencies:** Wait for external services
- **Configuration generation:** Create runtime configs

**Example: Laravel Optimization**
```yaml
hooks:
  pre-start:
    # Cache configuration
    - name: config-cache
      command: ["php", "artisan", "config:cache"]
      timeout: 30

    # Cache routes
    - name: route-cache
      command: ["php", "artisan", "route:cache"]
      timeout: 30

    # Cache views
    - name: view-cache
      command: ["php", "artisan", "view:cache"]
      timeout: 60

    # Run migrations
    - name: migrate
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

    # Create storage link
    - name: storage-link
      command: ["php", "artisan", "storage:link"]
      timeout: 10
```

## Global Post-Start Hooks

Execute **after** all processes have started and become healthy.

```yaml
hooks:
  post-start:
    - name: warmup-cache
      command: ["curl", "http://localhost/warmup"]
      timeout: 60

    - name: notify-deployment
      command: ["./notify-slack.sh", "Deployment complete"]
      timeout: 10
```

**Use Cases:**
- **Cache warming:** Populate application caches
- **Smoke tests:** Verify deployment success
- **Notifications:** Alert teams of successful deployment
- **Service registration:** Register with service discovery

## Process Pre-Stop Hooks

Execute **before** stopping an individual process.

```yaml
processes:
  horizon:
    command: ["php", "artisan", "horizon"]
    shutdown:
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60
```

**Use Cases:**
- **Graceful termination:** Signal processes to finish current work
- **Job completion:** Let workers finish processing
- **Connection draining:** Close connections gracefully
- **State persistence:** Save in-progress work

**Example: Laravel Horizon Graceful Shutdown**
```yaml
processes:
  horizon:
    command: ["php", "artisan", "horizon"]
    shutdown:
      timeout: 120  # Allow time for jobs to finish
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60  # Wait up to 60s for terminate signal
```

**How it works:**
1. Cbox Init sends `horizon:terminate` command
2. Horizon stops accepting new jobs
3. Horizon finishes currently running jobs
4. Horizon exits gracefully
5. If timeout expires, Cbox Init sends SIGTERM

## Process Post-Stop Hooks

Execute **after** stopping an individual process.

```yaml
processes:
  app:
    command: ["./my-app"]
    shutdown:
      post_stop_hook:
        command: ["./cleanup.sh"]
        timeout: 30
```

**Use Cases:**
- **Cleanup:** Remove temporary files
- **Logging:** Record shutdown completion
- **Resource release:** Free system resources
- **Notifications:** Alert monitoring systems

## Hook Execution Order

```
Container Start
    ↓
Global Pre-Start Hooks (sequential)
    ↓
Process Startup (by priority and depends_on)
    ↓
Wait for Health Checks
    ↓
Global Post-Start Hooks (sequential)
    ↓
... processes running ...
    ↓
Shutdown Signal (SIGTERM/SIGINT)
    ↓
Process Pre-Stop Hooks (parallel, per process)
    ↓
Process Shutdown (reverse priority order)
    ↓
Process Post-Stop Hooks (parallel, per process)
    ↓
Container Exit
```

## Advanced Patterns

### Conditional Execution

```bash
#!/bin/bash
# pre-start-hook.sh

# Only run migrations in production
if [ "$APP_ENV" = "production" ]; then
    php artisan migrate --force
fi

# Only seed in staging
if [ "$APP_ENV" = "staging" ]; then
    php artisan db:seed
fi
```

```yaml
hooks:
  pre-start:
    - name: environment-setup
      command: ["./pre-start-hook.sh"]
      timeout: 300
```

### Retry Logic

```bash
#!/bin/bash
# wait-for-database.sh

MAX_RETRIES=30
RETRY_DELAY=2

for i in $(seq 1 $MAX_RETRIES); do
    if php artisan db:ping; then
        echo "Database is ready!"
        exit 0
    fi
    echo "Waiting for database... ($i/$MAX_RETRIES)"
    sleep $RETRY_DELAY
done

echo "Database not available after $MAX_RETRIES retries"
exit 1
```

```yaml
hooks:
  pre-start:
    - name: wait-for-database
      command: ["./wait-for-database.sh"]
      timeout: 120  # 30 retries × 2s + buffer
```

### Multi-Step Preparation

```yaml
hooks:
  pre-start:
    # Step 1: Wait for dependencies
    - name: wait-dependencies
      command: ["./wait-for-services.sh"]
      timeout: 60

    # Step 2: Run migrations
    - name: migrate
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

    # Step 3: Verify migration
    - name: verify-schema
      command: ["php", "artisan", "schema:verify"]
      timeout: 30

    # Step 4: Optimize
    - name: optimize
      command: ["php", "artisan", "optimize"]
      timeout: 60
```

### Environment-Specific Hooks

```yaml
hooks:
  pre-start:
    - name: setup
      command: ["/setup-${APP_ENV}.sh"]  # Uses APP_ENV variable
      timeout: 120
```

**setup-production.sh:**
```bash
#!/bin/bash
set -e
php artisan migrate --force
php artisan config:cache
php artisan route:cache
php artisan view:cache
```

**setup-staging.sh:**
```bash
#!/bin/bash
set -e
php artisan migrate:fresh --seed --force
php artisan config:cache
```

## Complete Examples

### Laravel Production Setup

```yaml
version: "1.0"

hooks:
  pre-start:
    # Optimize Laravel
    - name: config-cache
      command: ["php", "artisan", "config:cache"]
      timeout: 30

    - name: route-cache
      command: ["php", "artisan", "route:cache"]
      timeout: 30

    - name: view-cache
      command: ["php", "artisan", "view:cache"]
      timeout: 60

    # Run migrations
    - name: migrate
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

    # Create storage link
    - name: storage-link
      command: ["php", "artisan", "storage:link"]
      timeout: 10

  post-start:
    # Warm application cache
    - name: cache-warmup
      command: ["php", "artisan", "cache:warmup"]
      timeout: 120

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    shutdown:
      timeout: 120
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60
```

### Database-Dependent Services

```yaml
hooks:
  pre-start:
    # Wait for database
    - name: wait-database
      command: ["./wait-for-postgres.sh"]
      timeout: 60

    # Run migrations
    - name: migrate
      command: ["./run-migrations.sh"]
      timeout: 300

    # Verify schema
    - name: verify
      command: ["./verify-schema.sh"]
      timeout: 30
```

**wait-for-postgres.sh:**
```bash
#!/bin/bash
until pg_isready -h localhost -p 5432 -U myuser; do
    echo "Waiting for PostgreSQL..."
    sleep 2
done
echo "PostgreSQL is ready!"
```

## Troubleshooting

### Hook Timeout

**Problem:** Hook exceeds timeout and container fails to start.

**Solution:**
```yaml
hooks:
  pre-start:
    - name: slow-migration
      timeout: 600  # Increase timeout to 10 minutes
```

### Hook Failure

**Problem:** Hook fails and prevents startup.

**Solution:** Add error handling to hook script:
```bash
#!/bin/bash
# Continue on non-critical errors
php artisan migrate --force || echo "Migration failed, continuing..."
php artisan config:cache || true
exit 0  # Always succeed
```

### Missing Dependencies

**Problem:** Hook runs before dependencies are available.

**Solution:** Add wait logic:
```bash
#!/bin/bash
# Wait for Redis
timeout 60 bash -c 'until redis-cli ping; do sleep 1; done'

# Then run hook
php artisan cache:clear
```

## Best Practices

### ✅ Do

- **Keep hooks idempotent:** Safe to run multiple times
- **Add timeouts:** Prevent hanging on failures
- **Use absolute paths:** Avoid working directory issues
- **Log output:** Make debugging easier
- **Handle errors:** Graceful degradation
- **Test independently:** Run hooks in isolation first

### ❌ Don't

- **Don't run daemons:** Hooks should exit when done
- **Don't assume timing:** Other processes may not be ready
- **Don't hardcode values:** Use environment variables
- **Don't ignore exit codes:** Failures should fail the hook
- **Don't make external dependencies required:** Have fallbacks

## See Also

- [Process Configuration](processes) - Process settings
- [Health Checks](health-checks) - Health monitoring
- [Examples](../examples/) - Real-world hook usage

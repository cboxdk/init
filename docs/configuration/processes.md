---
title: "Process Configuration"
description: "Configure process commands, dependencies, scaling, restart policies, and resource limits"
weight: 13
---

# Process Configuration

Complete reference for configuring individual processes managed by Cbox Init.

## Basic Process Structure

```yaml
processes:
  process-name:
    enabled: true
    command: ["executable", "arg1", "arg2"]
    restart: always
    scale: 1
    user: www-data         # Optional: run as user
    group: www-data        # Optional: run as group
    working_dir: /var/www/html
    env:
      KEY: value
```

## Core Settings

### enabled

**Type:** `boolean`
**Default:** `true`
**Description:** Whether to start this process.

```yaml
processes:
  nginx:
    enabled: true  # Start this process
```

**Use cases:**
- Temporarily disable processes without removing configuration
- Environment-specific processes (dev vs production)
- Feature flags for optional services

### command

**Type:** `array` of strings
**Required:** Yes
**Description:** Command and arguments to execute.

```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]  # Foreground mode, pool config, restart on-demand
```

**Important:**
- ✅ Use array format for proper argument parsing
- ✅ Run processes in foreground mode (no daemonizing)
- ❌ Don't use shell syntax like `["sh", "-c", "nginx -g 'daemon off;'"]`

**Common Commands:**
```yaml
# PHP-FPM
command: ["php-fpm", "-F", "-R"]

# Nginx
command: ["nginx", "-g", "daemon off;"]

# Laravel Queue Worker
command: ["php", "artisan", "queue:work", "--tries=3"]

# Laravel Horizon
command: ["php", "artisan", "horizon"]

# Symfony Messenger
command: ["php", "bin/console", "messenger:consume", "async"]

# WordPress Cron
command: ["php", "/var/www/html/wp-cron.php"]

# Custom PHP Worker
command: ["php", "/app/worker.php"]
```

### restart

**Type:** `string`
**Options:** `always`, `on-failure`, `never`
**Default:** `always`
**Description:** Restart policy when process exits.

```yaml
processes:
  php-fpm:
    restart: always  # Always restart on exit

  migration:
    restart: never  # One-time execution

  queue:
    restart: on-failure  # Only restart on non-zero exit
```

**Policy Guide:**
- `always` - Long-running services (PHP-FPM, Nginx, Horizon)
- `on-failure` - Services that should recover from errors but respect clean exits
- `never` - One-time tasks (migrations, seed data)

### scale

**Type:** `integer`
**Default:** `1`
**Description:** Number of identical instances to run.

```yaml
processes:
  queue-default:
    command: ["php", "artisan", "queue:work"]
    scale: 3  # Run 3 queue workers
```

**When to scale:**
- Queue workers (3-10 instances)
- Background processors
- Parallel job execution

**Automatic naming:**
- `queue-default-1`
- `queue-default-2`
- `queue-default-3`

## Advanced Settings

### user

**Type:** `string`
**Default:** None (inherit from parent process)
**Description:** Run process as specified user (name or numeric UID).

```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    user: www-data  # Run as www-data user
```

**Formats:**
- Username: `www-data`, `nginx`, `nobody`
- Numeric UID: `82`, `33`, `65534`

**Notes:**
- Requires root privileges to switch users
- If only `user` is specified, the user's primary group is used
- Implements s6-overlay compatible USER directive functionality

### group

**Type:** `string`
**Default:** User's primary group (if `user` specified)
**Description:** Run process as specified group (name or numeric GID).

```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    user: www-data
    group: www-data  # Explicit group
```

**Formats:**
- Group name: `www-data`, `nginx`, `nogroup`
- Numeric GID: `82`, `33`, `65534`

**Docker Example:**
```yaml
# Common pattern for Alpine-based images
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    user: "82"   # www-data UID on Alpine
    group: "82"  # www-data GID on Alpine

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    user: nginx
    group: nginx
```

**Security Best Practice:**
- Run services with minimal required privileges
- Use dedicated service users (www-data, nginx) instead of root
- Cbox Init will warn if running as root without user/group specified

### depends_on

**Type:** `array` of strings
**Default:** `[]`
**Description:** Process dependencies for startup ordering.

```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]  # Wait for PHP-FPM to be healthy

  horizon:
    command: ["php", "artisan", "horizon"]
    depends_on: [php-fpm]  # Wait for PHP-FPM
```

**Behavior:**
- Processes wait for dependencies to be **healthy** (if health check configured)
- Creates a dependency graph (DAG) for proper ordering
- Prevents startup failures from missing dependencies
- Processes without dependencies start in alphabetical order

### working_dir

**Type:** `string`
**Default:** `/var/www/html` or current directory
**Description:** Working directory for process execution.

```yaml
processes:
  app:
    command: ["./my-app"]
    working_dir: /opt/application
```

### stdout / stderr

**Type:** `bool`
**Default:** `true`
**Description:** Enable or disable forwarding of the process' STDOUT/STDERR streams to Cbox's logger.

```yaml
processes:
  queue-default:
    command: ["php", "artisan", "queue:work"]
    stdout: false  # Silence STDOUT
    stderr: true
```

Use these top-level flags as a shorthand for `logging.stdout` and `logging.stderr`.

### env

**Type:** `object`
**Default:** `{}`
**Description:** Environment variables for the process.

```yaml
processes:
  queue:
    command: ["php", "artisan", "queue:work"]
    env:
      QUEUE_CONNECTION: redis
      QUEUE_NAME: default
      REDIS_HOST: localhost
```

**Inheritance:**
- Process env vars override global environment
- System env vars are inherited by default

### schedule

**Type:** `string` (cron expression)
**Default:** None
**Description:** Run process on a schedule (cron-like).

```yaml
processes:
  backup:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"  # Daily at 2 AM
```

**Cron Format:** `minute hour day month weekday`

```
"*/15 * * * *"  # Every 15 minutes
"0 * * * *"     # Every hour
"0 2 * * *"     # Daily at 2 AM
"0 2 * * 1"     # Mondays at 2 AM
"0 0 1 * *"     # First day of month
```

See [Scheduled Tasks](../features/scheduled-tasks) for complete guide.

### schedule_timeout

**Type:** `string` (duration)
**Default:** No timeout
**Description:** Maximum execution time for scheduled task. Process is killed if timeout exceeded.

```yaml
processes:
  backup:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"
    schedule_timeout: "30m"  # Kill if runs longer than 30 minutes
```

**Duration Format:**
- `30s` - 30 seconds
- `5m` - 5 minutes
- `1h` - 1 hour
- `1h30m` - 1 hour 30 minutes

**Best Practice:** Set timeout less than schedule interval to prevent overlap.

### schedule_max_concurrent

**Type:** `integer`
**Default:** `0` (unlimited)
**Description:** Maximum concurrent executions of this scheduled task.

```yaml
processes:
  sync:
    command: ["php", "artisan", "sync:external-api"]
    schedule: "*/5 * * * *"
    schedule_max_concurrent: 1  # Skip if previous run still active
```

**Values:**
- `0` - Unlimited concurrent executions (default)
- `1` - No overlap (skip trigger if task still running)
- `N` - Allow up to N concurrent executions

**Use cases:**
- `1` for tasks that shouldn't overlap (database sync, backups)
- `0` or higher for idempotent tasks that can run in parallel

### schedule_timezone

**Type:** `string`
**Default:** `Local`
**Description:** Timezone for schedule interpretation.

```yaml
processes:
  report:
    command: ["php", "artisan", "reports:generate"]
    schedule: "0 9 * * 1-5"  # 9 AM weekdays
    schedule_timezone: "America/New_York"  # In Eastern time
```

**Values:**
- `Local` - System timezone (default)
- `UTC` - Coordinated Universal Time
- Any IANA timezone: `America/New_York`, `Europe/London`, `Asia/Tokyo`, etc.

## Shutdown Configuration

### shutdown.timeout

**Type:** `integer` (seconds)
**Default:** Inherits from `global.shutdown_timeout`
**Description:** Process-specific shutdown timeout.

```yaml
processes:
  horizon:
    command: ["php", "artisan", "horizon"]
    shutdown:
      timeout: 120  # Allow 2 minutes for graceful shutdown
```

### shutdown.pre_stop_hook

**Type:** `object`
**Description:** Command to run before stopping the process.

```yaml
processes:
  horizon:
    command: ["php", "artisan", "horizon"]
    shutdown:
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60
```

**Use cases:**
- Laravel Horizon graceful termination
- Database connection cleanup
- File upload completion
- Cache flush operations

See [Lifecycle Hooks](lifecycle-hooks) for pre/post start hooks.

## Health Check Configuration

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

**Health Check Types:**
- `tcp` - TCP port connection
- `http` - HTTP endpoint check
- `exec` - Execute command

See [Health Checks Configuration](health-checks) for complete reference.

## Heartbeat Monitoring

```yaml
processes:
  critical-backup:
    command: ["php", "artisan", "backup:critical"]
    schedule: "0 3 * * *"
    heartbeat:
      url: "https://hc-ping.com/your-uuid-here"
      timeout: 10
```

**Supported Services:**
- healthchecks.io
- Cronitor
- Better Uptime
- Custom endpoints

See [Heartbeat Monitoring](../features/heartbeat-monitoring) for complete guide.

## Complete Example

```yaml
processes:
  # Core Infrastructure
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
    user: www-data       # Run as www-data user
    group: www-data      # Run as www-data group
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      interval: 10

  # Web Server
  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    restart: always
    user: nginx          # Run as nginx user
    depends_on: [php-fpm]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"

  # Application Services
  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    restart: on-failure
    depends_on: [php-fpm]
    working_dir: /var/www/html
    shutdown:
      timeout: 120
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60

  # Queue Workers
  queue-default:
    enabled: true
    command: ["php", "artisan", "queue:work", "--tries=3"]
    scale: 3
    restart: always
    depends_on: [php-fpm]
    env:
      QUEUE_CONNECTION: redis
      QUEUE_NAME: default

  # Scheduled Tasks
  daily-backup:
    enabled: true
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"
    restart: never
    heartbeat:
      url: "https://hc-ping.com/backup-job-uuid"
```

## Environment Variable Overrides

Override process settings via environment variables:

```bash
# Enable/disable process
CBOX_INIT_PROCESS_NGINX_ENABLED=false

# Change scale
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=5

# Override command (JSON array)
CBOX_INIT_PROCESS_APP_COMMAND='["./my-app","--port=8080"]'
```

**Pattern:** `CBOX_INIT_PROCESS_<NAME>_<SETTING>=<value>`

See [Environment Variables](environment-variables) for complete reference.

## See Also

- [Health Checks](health-checks) - Configure health monitoring
- [Lifecycle Hooks](lifecycle-hooks) - Pre/post start/stop hooks
- [Environment Variables](environment-variables) - ENV var reference
- [Examples](../examples/) - Real-world configurations

---
title: "Scheduled Tasks"
description: "Configure cron-like scheduled tasks with per-task execution history and a schedule status API"
weight: 34
---

# Scheduled Tasks Example

Run periodic tasks like backups, reports, and maintenance jobs using Cbox Init's built-in cron scheduler.

## Use Cases

- ✅ Replace cron in Docker containers
- ✅ Database backups and maintenance
- ✅ Report generation and data sync
- ✅ Cache warming and optimization
- ✅ Cleanup and housekeeping tasks

## Cron Schedule Format

Cbox Init uses standard 5-field cron expressions:

```
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12)
│ │ │ │ ┌───────────── day of week (0-6, 0=Sunday)
│ │ │ │ │
* * * * *
```

**Special Characters:**
- `*` - Any value
- `,` - Value list separator (e.g., `1,3,5`)
- `-` - Range (e.g., `1-5`)
- `/` - Step (e.g., `*/15`)

## Common Schedule Patterns

```yaml
schedule: "* * * * *"       # Every minute
schedule: "*/15 * * * *"    # Every 15 minutes
schedule: "30 * * * *"      # Every hour at :30
schedule: "0 2 * * *"       # Daily at 2 AM
schedule: "0 8 * * 1"       # Every Monday at 8 AM
schedule: "0 0 1 * *"       # First day of month at midnight
schedule: "0 9-17 * * 1-5"  # Business hours (9 AM - 5 PM, Mon-Fri)
schedule: "0 3 * * 6,0"     # Weekend only (Sat-Sun at 3 AM)
```

## Complete Configuration

```yaml
version: "1.0"

global:
  log_format: json
  log_level: info
  api_enabled: true
  api_port: 9180

processes:
  # Database backup - Daily at 2 AM
  database-backup:
    enabled: true
    command: ["php", "artisan", "backup:database"]
    schedule: "0 2 * * *"
    schedule_timeout: "10m"
    restart: never
    env:
      BACKUP_PATH: /backups
      RETENTION_DAYS: "30"

  # Cache warming - Every 15 minutes
  cache-warmer:
    enabled: true
    command: ["php", "artisan", "cache:warm"]
    schedule: "*/15 * * * *"
    schedule_max_concurrent: 1
    restart: never

  # Reports - Hourly during business hours
  hourly-reports:
    enabled: true
    command: ["php", "artisan", "reports:generate"]
    schedule: "0 9-17 * * 1-5"  # 9 AM - 5 PM, Mon-Fri
    restart: never
    env:
      REPORT_TYPE: hourly
      OUTPUT_DIR: /var/www/storage/reports

  # Log rotation - Daily at midnight
  log-rotation:
    enabled: true
    command: ["/usr/local/bin/rotate-logs.sh"]
    schedule: "0 0 * * *"
    restart: never
    env:
      LOG_DIR: /var/log/app
      COMPRESS: "true"
      MAX_AGE_DAYS: "7"

  # Data sync - Every 30 minutes
  data-sync:
    enabled: true
    command: ["php", "artisan", "data:sync"]
    schedule: "*/30 * * * *"
    restart: never
    env:
      SYNC_SOURCE: remote-api
      SYNC_BATCH_SIZE: "100"

  # Weekly maintenance - Sunday at 3 AM
  weekly-maintenance:
    enabled: true
    command: ["/usr/local/bin/weekly-maintenance.sh"]
    schedule: "0 3 * * 0"  # Sunday
    restart: never
    env:
      OPTIMIZE_DATABASE: "true"
      CLEAR_OLD_SESSIONS: "true"
```

## Task Breakdown

### 1. Database Backup

```yaml
database-backup:
  command: ["php", "artisan", "backup:database"]
  schedule: "0 2 * * *"  # Daily at 2 AM
  schedule_timeout: "10m"
  restart: never
```

**Laravel Command:**
```php
// app/Console/Commands/BackupDatabase.php
class BackupDatabase extends Command
{
    protected $signature = 'backup:database';

    public function handle()
    {
        $filename = 'backup-' . date('Y-m-d') . '.sql';
        $path = env('BACKUP_PATH', '/backups');
        // ... backup logic
        $this->info("Backup created: {$path}/{$filename}");
        return 0;  // Success
    }
}
```

### 2. Cache Warming

```yaml
cache-warmer:
  command: ["php", "artisan", "cache:warm"]
  schedule: "*/15 * * * *"  # Every 15 minutes
  schedule_max_concurrent: 1
```

Runs every 15 minutes (00, 15, 30, 45). `schedule_max_concurrent: 1` skips a
trigger if the previous run is still active.

### 3. Hourly Reports

```yaml
hourly-reports:
  schedule: "0 9-17 * * 1-5"  # 9 AM - 5 PM, Mon-Fri
```

- `0` - At minute 0 (top of hour)
- `9-17` - Hours 9 through 17 (9 AM - 5 PM)
- `1-5` - Monday through Friday

## Inspecting Scheduled Tasks

Cbox Init records real per-task execution history and statistics. Enable the API
(`api_enabled: true`) and query it.

### Status

```bash
curl http://localhost:9180/api/v1/processes/database-backup/schedule
```

```json
{
  "process": "database-backup",
  "schedule": {
    "name": "database-backup",
    "schedule": "0 2 * * *",
    "timezone": "UTC",
    "state": "idle",
    "last_run": "2026-08-26T02:00:00Z",
    "next_run": "2026-08-27T02:00:00Z",
    "stats": {
      "total_executions": 42,
      "success_count": 41,
      "failure_count": 1,
      "running_count": 0,
      "success_rate": 97.62,
      "average_duration": 45200000000,
      "last_execution_time": "2026-08-26T02:00:00Z",
      "last_success_time": "2026-08-26T02:00:45Z",
      "last_failure_time": "2026-08-20T02:00:12Z"
    }
  }
}
```

`average_duration` is a Go duration in nanoseconds.

### History

```bash
curl "http://localhost:9180/api/v1/processes/database-backup/schedule/history?limit=5"
```

### Pause / Resume / Trigger

```bash
curl -X POST http://localhost:9180/api/v1/processes/database-backup/schedule/pause
curl -X POST http://localhost:9180/api/v1/processes/database-backup/schedule/resume
curl -X POST "http://localhost:9180/api/v1/processes/database-backup/schedule/trigger?sync=true"
```

## Environment Variables

Scheduled task processes receive these environment variables in addition to
their own `env`:

```bash
CBOX_INIT_PROCESS=database-backup   # The process name
CBOX_INIT_SCHEDULED=true            # Marks this as a scheduled run
```

**Use in scripts:**
```bash
#!/bin/bash
echo "Running scheduled task: $CBOX_INIT_PROCESS"
```

## Best Practices

### ✅ Do

**Idempotent Tasks:**
```bash
php artisan cache:clear    # Safe to run repeatedly
php artisan backup:create  # Creates a new backup each time
```

**Error Handling:**
```bash
#!/bin/bash
set -e  # Exit on error

if [ -z "$BACKUP_PATH" ]; then
    echo "ERROR: BACKUP_PATH not set"
    exit 1
fi

php artisan backup:run
echo "Backup completed successfully"
```

**Timeout Safety:**
```yaml
database-backup:
  schedule: "0 2 * * *"
  schedule_timeout: "10m"  # Cancel if it exceeds 10 minutes
```

### ❌ Don't

**Don't use long-running daemons:**
```yaml
# ❌ Bad - daemon processes don't work with schedule
schedule: "* * * * *"
command: ["./background-daemon"]  # Never exits

# ✅ Good - one-time execution
command: ["./process-batch-then-exit"]
```

**Don't forget `restart: never`:**
```yaml
# ❌ Bad - reruns immediately after finishing
scheduled-task:
  schedule: "0 2 * * *"
  restart: always

# ✅ Good - runs once per schedule
scheduled-task:
  schedule: "0 2 * * *"
  restart: never
```

## Real-World Examples

### PHP Framework Scheduled Tasks

```yaml
processes:
  # Laravel: use Laravel's built-in scheduler
  laravel-scheduler:
    command: ["php", "artisan", "schedule:run"]
    schedule: "* * * * *"
    restart: never

  # Symfony: use the messenger scheduler transport
  symfony-scheduler:
    command: ["php", "bin/console", "messenger:consume", "scheduler_default", "--time-limit=50"]
    schedule: "* * * * *"
    restart: never

  # WordPress: run WP-Cron via CLI
  wordpress-cron:
    command: ["php", "/var/www/html/wp-cron.php"]
    schedule: "*/5 * * * *"
    restart: never
```

### Database Maintenance

```yaml
db-optimize:
  command: ["php", "artisan", "db:optimize"]
  schedule: "0 4 * * 0"  # Sunday 4 AM
  restart: never
  env:
    DB_OPTIMIZE_TABLES: "users,orders,products"
```

### Stagger Multiple Tasks

```yaml
# Don't run everything at the same time
backup-database:
  schedule: "0 2 * * *"   # 2:00 AM
backup-files:
  schedule: "15 2 * * *"  # 2:15 AM
backup-logs:
  schedule: "30 2 * * *"  # 2:30 AM
```

## See Also

- [Scheduled Tasks (feature)](../features/scheduled-tasks) - Full scheduler reference
- [Process Configuration](../configuration/processes) - Process settings
- [Management API](../observability/api) - Runtime task inspection

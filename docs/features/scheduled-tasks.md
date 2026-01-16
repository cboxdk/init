---
title: "Scheduled Tasks"
description: "Built-in cron scheduler for periodic tasks with per-task statistics and heartbeat monitoring"
weight: 22
---

# Scheduled Tasks

Cbox Init includes a built-in cron-like scheduler for running periodic tasks without requiring a separate cron daemon.

## Overview

The scheduler provides:
- ✅ **Standard cron format:** Familiar 5-field syntax
- ✅ **Per-task statistics:** Track run count, success/failure rates, duration
- ✅ **Heartbeat integration:** External monitoring support (healthchecks.io, etc.)
- ✅ **Structured logging:** Task-specific logs with execution context
- ✅ **Graceful shutdown:** Running tasks cancelled cleanly on shutdown
- ✅ **No cron daemon:** Self-contained scheduling in Go

## Basic Configuration

```yaml
processes:
  backup-job:
    enabled: true
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"  # Daily at 2 AM
    restart: never  # Important for scheduled tasks
```

## Cron Schedule Format

### 5-Field Syntax

```
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12)
│ │ │ │ ┌───────────── day of week (0-6, Sunday=0)
│ │ │ │ │
* * * * *
```

### Special Characters

| Character | Meaning | Example |
|-----------|---------|---------|
| `*` | Any value | `* * * * *` = every minute |
| `,` | Value list | `1,15,30` = minutes 1, 15, 30 |
| `-` | Range | `1-5` = Monday through Friday |
| `/` | Step | `*/15` = every 15 minutes |

### Common Patterns

```yaml
# Every minute
schedule: "* * * * *"

# Every 5 minutes
schedule: "*/5 * * * *"

# Every 15 minutes
schedule: "*/15 * * * *"

# Every hour at :30
schedule: "30 * * * *"

# Daily at 2 AM
schedule: "0 2 * * *"

# Every weekday at 9 AM
schedule: "0 9 * * 1-5"

# First day of month
schedule: "0 0 1 * *"

# Every 6 hours
schedule: "0 */6 * * *"

# Twice daily (6 AM and 6 PM)
schedule: "0 6,18 * * *"

# Business hours (9-5, Mon-Fri)
schedule: "0 9-17 * * 1-5"

# Weekend mornings
schedule: "0 8 * * 0,6"
```

## Task Execution

### Execution Lifecycle

```
[Cron Trigger]
      ↓
[Check if already running]
      ↓
[Start task process]
      ↓
[Send start heartbeat] (optional)
      ↓
[Wait for completion]
      ↓
[Record statistics]
      ↓
[Send success/failure heartbeat] (optional)
      ↓
[Wait for next trigger]
```

### Environment Variables

Each scheduled task receives additional environment variables:

```bash
CBOX_INIT_PROCESS_NAME=backup-job
CBOX_INIT_INSTANCE_ID=backup-job-run-42
CBOX_INIT_SCHEDULED=true
CBOX_INIT_SCHEDULE="0 2 * * *"
CBOX_INIT_START_TIME=1732162800
```

**Use in scripts:**
```bash
#!/bin/bash
echo "Task: $CBOX_INIT_PROCESS_NAME"
echo "Instance: $CBOX_INIT_INSTANCE_ID"
echo "Started: $(date -d @$CBOX_INIT_START_TIME)"
```

## Statistics Tracking

### Per-Task Metrics

Cbox Init tracks execution statistics for each scheduled task:

- **Last run time:** When task last executed
- **Next run time:** When task will run next
- **Last exit code:** Most recent exit status
- **Run count:** Total executions
- **Success count:** Successful completions (exit 0)
- **Failure count:** Failed executions (exit ≠ 0)
- **Average duration:** Mean execution time

### Prometheus Metrics

```bash
# Last execution timestamp
cbox_init_scheduled_task_last_run_timestamp{task="backup-job"}

# Next scheduled execution
cbox_init_scheduled_task_next_run_timestamp{task="backup-job"}

# Last exit code
cbox_init_scheduled_task_last_exit_code{task="backup-job"}

# Execution duration (histogram)
cbox_init_scheduled_task_duration_seconds{task="backup-job"}

# Total runs by status
cbox_init_scheduled_task_total{task="backup-job",status="success"}
cbox_init_scheduled_task_total{task="backup-job",status="failure"}
```

### Management API

```bash
# Get task status
curl http://localhost:9180/api/v1/processes | \
  jq '.[] | select(.scheduled==true)'

# Response:
{
  "name": "backup-job",
  "scheduled": true,
  "schedule": "0 2 * * *",
  "state": "waiting",
  "last_run": "2024-11-21T02:00:00Z",
  "next_run": "2024-11-22T02:00:00Z",
  "last_exit_code": 0,
  "run_count": 30,
  "success_count": 29,
  "failure_count": 1,
  "avg_duration": 45.2
}
```

## Heartbeat Monitoring

### Configuration

```yaml
processes:
  critical-backup:
    command: ["php", "artisan", "backup:critical"]
    schedule: "0 3 * * *"
    heartbeat:
      success_url: https://hc-ping.com/your-uuid-here
      failure_url: https://hc-ping.com/your-uuid-here/fail
      timeout: 10
      retry_count: 3
```

### Heartbeat Flow

```
[Task Starts]
      ↓
[Ping: /start] (optional)
      ↓
[Task Executes]
      ↓
[Exit Code 0?]
  ├─ Yes → [Ping: success_url]
  └─ No  → [Ping: failure_url]
```

### Supported Services

**healthchecks.io:**
```yaml
heartbeat:
  success_url: https://hc-ping.com/uuid
  failure_url: https://hc-ping.com/uuid/fail
```

**Cronitor:**
```yaml
heartbeat:
  success_url: https://cronitor.link/p/key/job-name
  failure_url: https://cronitor.link/p/key/job-name/fail
```

**Better Uptime:**
```yaml
heartbeat:
  success_url: https://betteruptime.com/api/v1/heartbeat/uuid
```

**Custom Endpoint:**
```yaml
heartbeat:
  success_url: https://monitoring.example.com/ping/backup
  method: POST
  headers:
    Authorization: Bearer your-token
    X-Task: database-backup
  timeout: 30
  retry_count: 5
```

## Complete Example

```yaml
version: "1.0"

global:
  metrics_enabled: true
  metrics_port: 9090

processes:
  # Database backup - Daily at 2 AM
  database-backup:
    enabled: true
    command: ["php", "artisan", "backup:database"]
    schedule: "0 2 * * *"
    restart: never
    env:
      BACKUP_PATH: /backups
      RETENTION_DAYS: "30"
    heartbeat:
      success_url: https://hc-ping.com/backup-uuid
      failure_url: https://hc-ping.com/backup-uuid/fail
      timeout: 30

  # Cache warming - Every 15 minutes
  cache-warmer:
    enabled: true
    command: ["php", "artisan", "cache:warm"]
    schedule: "*/15 * * * *"
    restart: never

  # Reports - Hourly during business hours
  hourly-reports:
    enabled: true
    command: ["php", "artisan", "reports:generate"]
    schedule: "0 9-17 * * 1-5"  # 9 AM - 5 PM, Mon-Fri
    restart: never
    heartbeat:
      success_url: https://hc-ping.com/reports-uuid
      timeout: 60

  # Weekly maintenance - Sunday at 3 AM
  weekly-maintenance:
    enabled: true
    command: ["/usr/local/bin/maintenance.sh"]
    schedule: "0 3 * * 0"  # Sunday
    restart: never
    env:
      OPTIMIZE_DATABASE: "true"
    heartbeat:
      success_url: https://hc-ping.com/weekly-uuid
      timeout: 300
```

## Advanced Features

### Concurrency Control

Cbox Init provides native concurrency controls for scheduled tasks via configuration options.

#### schedule_max_concurrent

Prevents task overlap by limiting concurrent executions:

```yaml
processes:
  database-sync:
    command: ["php", "artisan", "sync:database"]
    schedule: "*/5 * * * *"  # Every 5 minutes
    schedule_max_concurrent: 1  # Skip if previous run still active
```

**Values:**
- `0` - Unlimited concurrent executions (default)
- `1` - No overlap (skip trigger if task still running)
- `N` - Allow up to N concurrent executions

#### schedule_timeout

Kills tasks that exceed a maximum execution time:

```yaml
processes:
  backup:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"
    schedule_timeout: "30m"  # Kill if runs longer than 30 minutes
```

**Duration formats:** `30s`, `5m`, `1h`, `1h30m`

**Best practice:** Set timeout less than schedule interval to prevent overlap.

#### Combined Example

```yaml
processes:
  long-task:
    command: ["php", "artisan", "process:large-dataset"]
    schedule: "0 * * * *"  # Every hour
    schedule_timeout: "55m"  # Kill if exceeds 55 minutes
    schedule_max_concurrent: 1  # No overlap
    restart: never
```

#### Alternative: Application-Level Control

**Option 1: Use max-time in command**
```yaml
long-task:
  command: ["php", "artisan", "process:large-dataset", "--max-time=3500"]
  schedule: "0 * * * *"  # Every hour
```

**Option 2: Lock file in script**
```bash
#!/bin/bash
LOCKFILE="/tmp/my-task.lock"

if [ -f "$LOCKFILE" ]; then
    echo "Task already running"
    exit 0
fi

touch "$LOCKFILE"
trap "rm -f $LOCKFILE" EXIT

# Do work
php artisan expensive:task
```

### Retry Logic

```bash
#!/bin/bash
# task-with-retry.sh
MAX_RETRIES=3

for i in $(seq 1 $MAX_RETRIES); do
    if php artisan sync:external-api; then
        echo "Sync successful"
        exit 0
    fi
    echo "Attempt $i failed, retrying..."
    sleep 10
done

echo "All retries failed"
exit 1
```

```yaml
data-sync:
  command: ["/task-with-retry.sh"]
  schedule: "*/30 * * * *"
  heartbeat:
    failure_url: https://hc-ping.com/uuid/fail
```

### Conditional Execution

```bash
#!/bin/bash
# conditional-task.sh

# Only run on first Monday of month
DAY=$(date +%d)
WEEKDAY=$(date +%u)

if [ "$DAY" -le 7 ] && [ "$WEEKDAY" -eq 1 ]; then
    echo "First Monday - running monthly report"
    php artisan reports:monthly
else
    echo "Not first Monday - skipping"
fi
```

```yaml
monthly-report:
  command: ["/conditional-task.sh"]
  schedule: "0 8 * * 1"  # Every Monday at 8 AM
```

## Monitoring & Alerting

### Alert on Task Failure

```yaml
# Prometheus alert
- alert: ScheduledTaskFailed
  expr: cbox_init_scheduled_task_last_exit_code != 0
  for: 5m
  annotations:
    summary: "Task {{ $labels.task }} failed"
    description: "Exit code: {{ $value }}"
```

### Alert on Missed Execution

```yaml
# Alert if task hasn't run in expected interval
- alert: TaskNotRunning
  expr: time() - cbox_init_scheduled_task_last_run_timestamp > 86400
  annotations:
    summary: "Task {{ $labels.task }} hasn't run in 24h"
```

### Dashboard Panels

```promql
# Task execution success rate
sum(cbox_init_scheduled_task_total{status="success"}) /
sum(cbox_init_scheduled_task_total) * 100

# Average task duration
avg(cbox_init_scheduled_task_duration_seconds)

# Next run time (time until next execution)
cbox_init_scheduled_task_next_run_timestamp - time()
```

## Laravel Scheduler Integration

### Option 1: Cbox Init Native Scheduling

```yaml
# Define each task separately in Cbox Init config
processes:
  backup-daily:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"

  emails-hourly:
    command: ["php", "artisan", "emails:send"]
    schedule: "0 * * * *"

  cache-cleanup:
    command: ["php", "artisan", "cache:prune"]
    schedule: "0 0 * * *"
```

**Pros:**
- Individual task monitoring
- Per-task heartbeats
- Direct control over schedule
- Task-specific resource limits

### Option 2: Laravel Scheduler

```yaml
# Use Laravel's built-in scheduler
processes:
  laravel-scheduler:
    enabled: true
    command: ["php", "artisan", "schedule:work"]  # Or schedule:run with cron
    restart: always  # Keep scheduler running
```

**app/Console/Kernel.php:**
```php
protected function schedule(Schedule $schedule)
{
    $schedule->command('backup:run')->daily();
    $schedule->command('emails:send')->hourly();
    $schedule->command('cache:prune')->daily();
}
```

**Pros:**
- Centralized task definition in code
- Laravel's fluent schedule API
- Conditional scheduling logic
- Built-in overlap prevention

**Cons:**
- Single point of failure (scheduler process)
- No per-task heartbeat monitoring
- All tasks share same logs

### Hybrid Approach

```yaml
# Critical tasks: Cbox Init native (with heartbeats)
processes:
  critical-backup:
    command: ["php", "artisan", "backup:critical"]
    schedule: "0 3 * * *"
    heartbeat:
      success_url: https://hc-ping.com/critical-uuid

  # Non-critical tasks: Laravel scheduler
  laravel-scheduler:
    command: ["php", "artisan", "schedule:work"]
    restart: always
```

## Task Statistics

### Via Prometheus

```bash
# Last run time
curl http://localhost:9090/metrics | \
  grep 'cbox_init_scheduled_task_last_run_timestamp{task="backup-job"}'

# Success count
curl http://localhost:9090/metrics | \
  grep 'cbox_init_scheduled_task_total{task="backup-job",status="success"}'

# Average duration
curl http://localhost:9090/metrics | \
  grep 'cbox_init_scheduled_task_duration_seconds'
```

### Via Management API

```bash
# Get all scheduled tasks
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9180/api/v1/processes | \
  jq '.[] | select(.scheduled==true) | {name, last_run, next_run, success_count, failure_count}'
```

## Troubleshooting

### Task Not Running

**Check schedule parsing:**
```bash
# Validate cron expression
# Use https://crontab.guru or similar

# Check logs for schedule confirmation
docker logs app | grep "Scheduled task registered"
```

**Verify task is enabled:**
```yaml
my-task:
  enabled: true  # Must be true
  schedule: "0 2 * * *"
```

### Task Runs Multiple Times

**Problem:** Task configured with `restart: always`

**Solution:**
```yaml
scheduled-task:
  schedule: "0 2 * * *"
  restart: never  # REQUIRED for scheduled tasks
```

### Task Timeout

**Problem:** Task doesn't complete before next trigger

**Solution:**
```yaml
slow-task:
  schedule: "0 * * * *"  # Every hour
  timeout: 3500  # 58 minutes (less than interval)
```

### Missed Executions

**Check system time:**
```bash
# Verify container time is correct
docker exec app date

# Check timezone
docker exec app date +%Z
```

**Set timezone:**
```yaml
services:
  app:
    environment:
      TZ: "America/New_York"
```

## Best Practices

### ✅ Do

**Always use restart: never:**
```yaml
scheduled-task:
  schedule: "0 2 * * *"
  restart: never  # Tasks should not auto-restart
```

**Add heartbeat monitoring for critical tasks:**
```yaml
critical-backup:
  schedule: "0 3 * * *"
  heartbeat:
    success_url: https://hc-ping.com/uuid
    failure_url: https://hc-ping.com/uuid/fail
```

**Set appropriate timeouts:**
```yaml
backup-task:
  schedule: "0 2 * * *"
  timeout: 1800  # 30 minutes max
```

**Make tasks idempotent:**
```bash
# Safe to run multiple times
php artisan cache:clear  # Idempotent
php artisan backup:create  # Creates new backup each time
```

### ❌ Don't

**Don't use restart: always:**
```yaml
# ❌ Bad - task will run immediately after completion
task:
  schedule: "0 2 * * *"
  restart: always

# ✅ Good
task:
  schedule: "0 2 * * *"
  restart: never
```

**Don't run daemon processes:**
```yaml
# ❌ Bad - daemons don't work with schedule
task:
  schedule: "* * * * *"
  command: ["./background-daemon"]  # Never exits!

# ✅ Good - one-time execution
task:
  schedule: "* * * * *"
  command: ["./process-batch-then-exit"]  # Runs and exits
```

**Don't forget timeout < interval:**
```yaml
# ❌ Bad - task might overlap
task:
  schedule: "0 * * * *"  # Every hour
  timeout: 7200  # 2 hours!

# ✅ Good
task:
  schedule: "0 * * * *"  # Every hour
  timeout: 3500  # 58 minutes
```

## Real-World Examples

### Database Backup with Rotation

```yaml
database-backup:
  command: ["/backup-with-rotation.sh"]
  schedule: "0 2 * * *"  # Daily at 2 AM
  restart: never
  env:
    BACKUP_DIR: /backups
    KEEP_DAYS: "7"
    S3_BUCKET: my-backups
  heartbeat:
    success_url: https://hc-ping.com/backup-uuid
    timeout: 600  # 10 minute backup timeout
```

**backup-with-rotation.sh:**
```bash
#!/bin/bash
set -e

BACKUP_FILE="db-$(date +%Y%m%d-%H%M%S).sql.gz"

# Create backup
mysqldump -h database -u root -p"$DB_PASSWORD" laravel | gzip > "/tmp/$BACKUP_FILE"

# Upload to S3
aws s3 cp "/tmp/$BACKUP_FILE" "s3://$S3_BUCKET/backups/"

# Delete old local backups
find "$BACKUP_DIR" -name "*.sql.gz" -mtime +$KEEP_DAYS -delete

# Delete old S3 backups
aws s3 ls "s3://$S3_BUCKET/backups/" | \
  awk '{print $4}' | \
  while read file; do
    # ... delete old files
  done

echo "Backup complete: $BACKUP_FILE"
```

### API Data Sync with Retry

```yaml
api-sync:
  command: ["/sync-with-retry.sh"]
  schedule: "*/30 * * * *"  # Every 30 minutes
  restart: never
  env:
    API_ENDPOINT: https://api.example.com/data
    API_KEY: ${EXTERNAL_API_KEY}
  heartbeat:
    success_url: https://hc-ping.com/sync-uuid
    failure_url: https://hc-ping.com/sync-uuid/fail
    retry_count: 3
```

### Report Generation

```yaml
daily-report:
  command: ["php", "artisan", "reports:daily"]
  schedule: "0 8 * * *"  # Daily at 8 AM
  restart: never
  env:
    REPORT_FORMAT: pdf
    EMAIL_RECIPIENTS: team@example.com
  heartbeat:
    success_url: https://hc-ping.com/reports-uuid
```

## See Also

- [Process Configuration](../configuration/processes) - Schedule configuration
- [Heartbeat Monitoring](heartbeat-monitoring) - External monitoring
- [Examples](../examples/scheduled-tasks) - Practical examples
- [Prometheus Metrics](../observability/metrics) - Task metrics

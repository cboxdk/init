---
title: "Scheduled Tasks"
description: "Built-in cron scheduler for periodic tasks with per-task execution history and a schedule status API"
weight: 23
---

# Scheduled Tasks

Cbox Init includes a built-in cron-like scheduler for running periodic tasks without requiring a separate cron daemon.

## Overview

The scheduler provides:
- ✅ **Standard cron format:** Familiar 5-field syntax
- ✅ **Per-task execution history:** Bounded ring buffer of past runs with exit codes and durations
- ✅ **Per-task statistics:** Run/success/failure counts, success rate, average duration
- ✅ **Overlap protection:** A job never runs concurrently with itself by default
- ✅ **Pause / resume / manual trigger:** Over the management API
- ✅ **Graceful shutdown:** Running tasks are cancelled cleanly on shutdown
- ✅ **No cron daemon:** Self-contained scheduling in Go

> **Not implemented:** outbound heartbeat pings to services such as
> healthchecks.io, and Prometheus metrics for scheduled tasks. Query the
> [schedule status API](#schedule-status-api) instead. See
> [Heartbeat Monitoring](../observability/heartbeat-monitoring) for the current status.

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
schedule: "* * * * *"       # Every minute
schedule: "*/5 * * * *"     # Every 5 minutes
schedule: "30 * * * *"      # Every hour at :30
schedule: "0 2 * * *"       # Daily at 2 AM
schedule: "0 9 * * 1-5"     # Every weekday at 9 AM
schedule: "0 0 1 * *"       # First day of month
schedule: "0 6,18 * * *"    # Twice daily (6 AM and 6 PM)
schedule: "0 9-17 * * 1-5"  # Business hours (9-5, Mon-Fri)
```

## Schedule Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schedule` | string | — | 5-field cron expression |
| `schedule_timezone` | string | `UTC` | `UTC`, `Local`, or an IANA name (`America/New_York`) |
| `schedule_timeout` | duration | none | Kill the run if it exceeds this (`30s`, `5m`, `1h`) |
| `schedule_max_concurrent` | int | `0` | `0`/`1` = no overlap; `>1` = allow N parallel runs |

The retained history size per job is set globally with `schedule_history_size`
(default `100`).

## Task Execution

### Execution Lifecycle

```
[Cron Trigger]
      ↓
[Skip if already running] (overlap protection)
      ↓
[Start task process]
      ↓
[Wait for completion or timeout]
      ↓
[Record execution in history]
      ↓
[Wait for next trigger]
```

A job's state is one of `idle`, `executing`, or `paused`.

### Environment Variables

Each scheduled task process receives these environment variables in addition to
its own `env`:

```bash
CBOX_INIT_PROCESS=backup-job   # The process name
CBOX_INIT_SCHEDULED=true       # Marks this as a scheduled run
```

**Use in scripts:**
```bash
#!/bin/bash
echo "Scheduled task: $CBOX_INIT_PROCESS"
```

## Concurrency Control

### schedule_max_concurrent

Prevents task overlap by limiting concurrent executions:

```yaml
processes:
  database-sync:
    command: ["php", "artisan", "sync:database"]
    schedule: "*/5 * * * *"  # Every 5 minutes
    schedule_max_concurrent: 1  # Skip if previous run still active
```

**Values:**
- `0` or `1` - No overlap (skip trigger if the previous run is still active)
- `N` (>1) - Allow up to N concurrent executions

### schedule_timeout

Kills tasks that exceed a maximum execution time:

```yaml
processes:
  backup:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"
    schedule_timeout: "30m"  # Cancel if it runs longer than 30 minutes
```

**Duration formats:** `30s`, `5m`, `1h`, `1h30m`

**Best practice:** Set the timeout below the schedule interval to prevent overlap.

### Combined Example

```yaml
processes:
  long-task:
    command: ["php", "artisan", "process:large-dataset"]
    schedule: "0 * * * *"        # Every hour
    schedule_timeout: "55m"      # Cancel if it exceeds 55 minutes
    schedule_max_concurrent: 1   # No overlap
    restart: never
```

## Schedule Status API

Cbox Init records real per-task execution history and statistics, exposed over
the [management API](../observability/api) (enable it with `api_enabled: true`).

### Status

```bash
curl http://localhost:9180/api/v1/processes/backup-job/schedule
```

```json
{
  "process": "backup-job",
  "schedule": {
    "name": "backup-job",
    "schedule": "0 2 * * *",
    "timezone": "UTC",
    "state": "idle",
    "last_run": "2026-08-26T02:00:00Z",
    "next_run": "2026-08-27T02:00:00Z",
    "stats": {
      "total_executions": 30,
      "success_count": 29,
      "failure_count": 1,
      "running_count": 0,
      "success_rate": 96.67,
      "average_duration": 45200000000,
      "last_execution_time": "2026-08-26T02:00:00Z",
      "last_success_time": "2026-08-26T02:00:45Z",
      "last_failure_time": "2026-08-20T02:00:12Z"
    }
  }
}
```

`average_duration` is a Go duration in nanoseconds (here ≈ 45.2s).

### History

```bash
curl "http://localhost:9180/api/v1/processes/backup-job/schedule/history?limit=5"
```

```json
{
  "process": "backup-job",
  "limit": 5,
  "count": 1,
  "history": [
    {
      "id": 42,
      "start_time": "2026-08-26T02:00:00Z",
      "end_time": "2026-08-26T02:00:45Z",
      "exit_code": 0,
      "success": true,
      "error": "",
      "triggered": "schedule"
    }
  ]
}
```

`triggered` is `schedule` for cron-driven runs and `manual` for API triggers.

### Pause, Resume, Trigger

```bash
# Pause (skip scheduled triggers until resumed)
curl -X POST http://localhost:9180/api/v1/processes/backup-job/schedule/pause

# Resume
curl -X POST http://localhost:9180/api/v1/processes/backup-job/schedule/resume

# Trigger now (async; returns immediately)
curl -X POST http://localhost:9180/api/v1/processes/backup-job/schedule/trigger

# Trigger now and wait for the exit code (synchronous)
curl -X POST "http://localhost:9180/api/v1/processes/backup-job/schedule/trigger?sync=true"
```

## Complete Example

```yaml
version: "1.0"

global:
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

  # Weekly maintenance - Sunday at 3 AM
  weekly-maintenance:
    enabled: true
    command: ["/usr/local/bin/maintenance.sh"]
    schedule: "0 3 * * 0"  # Sunday
    restart: never
    env:
      OPTIMIZE_DATABASE: "true"
```

## Laravel Scheduler Integration

### Option 1: Cbox Init Native Scheduling

```yaml
processes:
  backup-daily:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"
    restart: never

  emails-hourly:
    command: ["php", "artisan", "emails:send"]
    schedule: "0 * * * *"
    restart: never
```

**Pros:** individual task monitoring via the schedule API, per-task timeouts, direct control over each schedule.

### Option 2: Laravel Scheduler

```yaml
processes:
  laravel-scheduler:
    enabled: true
    command: ["php", "artisan", "schedule:run"]
    schedule: "* * * * *"  # Let Laravel decide which tasks run
    restart: never
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

**Trade-off:** centralised task definition in code, but all tasks share one process and one execution-history entry per minute.

## Best Practices

### ✅ Do

**Always use `restart: never`** so a completed task is not restarted:
```yaml
scheduled-task:
  schedule: "0 2 * * *"
  restart: never
```

**Bound long tasks with `schedule_timeout`** and keep it below the interval:
```yaml
hourly-task:
  schedule: "0 * * * *"   # Every hour
  schedule_timeout: "55m" # Below the 60-minute interval
```

**Make tasks idempotent** so retries and manual triggers are safe.

### ❌ Don't

**Don't use `restart: always`** — the task would rerun immediately after finishing:
```yaml
# ❌ Bad
task:
  schedule: "0 2 * * *"
  restart: always

# ✅ Good
task:
  schedule: "0 2 * * *"
  restart: never
```

**Don't run daemons on a schedule** — scheduled commands must exit:
```yaml
# ❌ Bad — never exits
task:
  schedule: "* * * * *"
  command: ["./background-daemon"]

# ✅ Good — runs and exits
task:
  schedule: "* * * * *"
  command: ["./process-batch-then-exit"]
```

## Troubleshooting

### Task Not Running

- Validate the cron expression (e.g. with crontab.guru).
- Confirm the process is `enabled: true`.
- Check the logs for the scheduler registering the job.

### Task Runs Multiple Times

Set `restart: never` — a scheduled task with `restart: always` reruns immediately after completing.

### Task Overlaps Itself

Set `schedule_max_concurrent: 1` (the default already skips overlapping runs) and/or a `schedule_timeout` below the interval.

## See Also

- [Process Configuration](../configuration/processes) - Schedule configuration
- [Management API](../observability/api) - Runtime task inspection and control
- [Examples](../examples/scheduled-tasks) - Practical examples

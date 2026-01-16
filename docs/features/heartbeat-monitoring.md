---
title: "Heartbeat Monitoring"
description: "Integrate with external monitoring services for dead man's switch alerts and task completion tracking"
weight: 26
---

# Heartbeat Monitoring

Integrate Cbox Init with external monitoring services like healthchecks.io, Cronitor, or Better Uptime for dead man's switch monitoring.

## Overview

Heartbeat monitoring provides:
- ✅ **Dead man's switch:** Get alerted if tasks don't run
- ✅ **Failure notifications:** Immediate alerts on task failures
- ✅ **Execution tracking:** Monitor task duration and timing
- ✅ **External validation:** Independent monitoring outside your infrastructure
- ✅ **Multi-service support:** Works with healthchecks.io, Cronitor, Better Uptime, custom endpoints

## How It Works

### Heartbeat Flow

```
[Scheduled Task Triggers]
        ↓
[Task Starts]
        ↓
[Ping: /start] (optional)
        ↓
[Task Executes]
        ↓
[Task Completes]
        ↓
   Exit Code?
    ├─ 0 (success) → [Ping: success_url]
    └─ ≠0 (failure) → [Ping: failure_url with exit code]
```

### Monitoring Service Response

**If ping arrives on time:**
- ✅ Service marks check as successful
- ✅ No alert sent

**If ping doesn't arrive (task failed/hung/didn't run):**
- ⚠️ Service detects missed heartbeat
- 🚨 Alert sent via configured channels (email, Slack, PagerDuty, etc.)

## Basic Configuration

```yaml
processes:
  critical-backup:
    enabled: true
    command: ["php", "artisan", "backup:critical"]
    schedule: "0 3 * * *"  # Daily at 3 AM
    heartbeat:
      url: "https://hc-ping.com/your-uuid-here"
      timeout: 10
```

**Simplified config (single URL):**
- `url` - Ping on both success and failure
- Service determines success/failure from timing

## Advanced Configuration

### Separate Success/Failure URLs

```yaml
processes:
  backup-job:
    command: ["php", "artisan", "backup:run"]
    schedule: "0 2 * * *"
    heartbeat:
      success_url: https://hc-ping.com/uuid
      failure_url: https://hc-ping.com/uuid/fail
      timeout: 30
```

**Behavior:**
- Exit 0 → Ping `success_url`
- Exit ≠0 → Ping `failure_url` with exit code

### With Retry

```yaml
processes:
  flaky-sync:
    command: ["./sync-external-api.sh"]
    schedule: "*/30 * * * *"
    heartbeat:
      success_url: https://hc-ping.com/uuid
      timeout: 30
      retry_count: 3  # Retry ping 3 times
      retry_delay: 5  # Wait 5s between retries
```

**Use case:** Prevent false alerts from transient network issues.

### With Custom Headers

```yaml
processes:
  authenticated-task:
    command: ["./task.sh"]
    schedule: "0 * * * *"
    heartbeat:
      success_url: https://monitoring.example.com/heartbeat
      method: POST  # Default is POST
      headers:
        Authorization: Bearer your-token-here
        X-Service: cbox-init
        X-Environment: production
      timeout: 10
```

## Supported Services

### healthchecks.io

**Setup:**
1. Create check at https://healthchecks.io
2. Get ping URL: `https://hc-ping.com/your-uuid-here`
3. Configure schedule and grace period (matches your task interval)

**Configuration:**
```yaml
heartbeat:
  success_url: https://hc-ping.com/your-uuid-here
  failure_url: https://hc-ping.com/your-uuid-here/fail
```

**Features:**
- Free for up to 20 checks
- Email, SMS, Slack, PagerDuty, Webhook integrations
- Tracks execution duration
- Dashboard and API

### Cronitor

**Setup:**
1. Create monitor at https://cronitor.io
2. Get ping URL: `https://cronitor.link/p/your-key/job-name`

**Configuration:**
```yaml
heartbeat:
  success_url: https://cronitor.link/p/key/backup-job
  failure_url: https://cronitor.link/p/key/backup-job/fail
```

**Features:**
- Automatic schedule detection
- Anomaly detection
- Integration with incident management tools
- Team collaboration features

### Better Uptime

**Setup:**
1. Create heartbeat at https://betteruptime.com
2. Get URL: `https://betteruptime.com/api/v1/heartbeat/uuid`

**Configuration:**
```yaml
heartbeat:
  success_url: https://betteruptime.com/api/v1/heartbeat/your-uuid
```

**Features:**
- Incident management
- On-call scheduling
- Status pages
- Phone call alerts

### Custom Endpoint

**Build your own monitoring:**

```yaml
heartbeat:
  success_url: https://monitoring.example.com/ping/backup
  failure_url: https://monitoring.example.com/ping/backup/fail
  method: POST
  headers:
    Authorization: Bearer ${MONITORING_TOKEN}
    X-Task-Name: database-backup
    X-Host: ${HOSTNAME}
  timeout: 30
```

**Server endpoint:**
```php
// routes/api.php
Route::post('/ping/{task}', function (Request $request, $task) {
    $exitCode = $request->input('exit_code', 0);
    $duration = $request->input('duration', 0);

    TaskExecution::create([
        'task_name' => $task,
        'status' => $exitCode === 0 ? 'success' : 'failure',
        'exit_code' => $exitCode,
        'duration' => $duration,
        'timestamp' => now(),
    ]);

    return response()->json(['status' => 'recorded']);
});
```

## Complete Examples

### Critical Database Backup

```yaml
database-backup:
  command: ["php", "artisan", "backup:database"]
  schedule: "0 2 * * *"  # Daily at 2 AM
  restart: never
  env:
    BACKUP_PATH: /backups
    S3_BUCKET: critical-backups
  heartbeat:
    success_url: https://hc-ping.com/db-backup-uuid
    failure_url: https://hc-ping.com/db-backup-uuid/fail
    timeout: 600  # 10-minute backup timeout
    retry_count: 3
```

**What happens:**
- ✅ Backup runs at 2 AM → Ping success → No alert
- ❌ Backup fails → Ping failure → Alert sent
- ❌ Backup doesn't run → No ping → Alert sent after grace period

### API Data Sync

```yaml
api-sync:
  command: ["/sync-with-retry.sh"]
  schedule: "*/15 * * * *"  # Every 15 minutes
  restart: never
  heartbeat:
    success_url: https://cronitor.link/p/key/api-sync
    failure_url: https://cronitor.link/p/key/api-sync/fail
    timeout: 120
    retry_count: 5
    retry_delay: 10
```

### Weekly Report Generation

```yaml
weekly-report:
  command: ["php", "artisan", "reports:weekly"]
  schedule: "0 8 * * 1"  # Monday 8 AM
  restart: never
  heartbeat:
    success_url: https://betteruptime.com/api/v1/heartbeat/reports-uuid
    timeout: 300  # 5-minute generation timeout
```

## Monitoring Multiple Tasks

### Centralized Dashboard

**healthchecks.io:**
```
Dashboard shows all tasks:
✅ database-backup - Last ping: 2 hours ago
✅ api-sync - Last ping: 5 minutes ago
❌ weekly-report - LATE: Expected 8 hours ago
⚠️ cache-warmer - Failing (exit code 1)
```

### Grouped Monitoring

```yaml
# Group related tasks
backup-database:
  heartbeat:
    success_url: https://hc-ping.com/backup-db-uuid
    headers:
      X-Group: backups

backup-files:
  heartbeat:
    success_url: https://hc-ping.com/backup-files-uuid
    headers:
      X-Group: backups

backup-logs:
  heartbeat:
    success_url: https://hc-ping.com/backup-logs-uuid
    headers:
      X-Group: backups
```

## Alert Configuration

### healthchecks.io Alerts

**Configure alerting:**
1. Email: Immediate notification
2. Slack: Post to #alerts channel
3. PagerDuty: Page on-call engineer (for critical tasks)
4. Webhook: Custom integration

**Alert conditions:**
- Task doesn't ping within schedule + grace period
- Task pings /fail endpoint
- Task duration exceeds expected time

### Cronitor Alerts

**Smart alerting:**
- Anomaly detection (task usually takes 5 min, took 30 min)
- Schedule detection (learns expected run times)
- Failure pattern recognition

### Custom Alerts

```yaml
heartbeat:
  failure_url: https://monitoring.example.com/alert
  headers:
    X-Alert-Channels: "slack,pagerduty,email"
    X-Severity: critical
    X-Escalation-Policy: immediate
```

## Troubleshooting

### Heartbeat Not Pinging

**Test URL manually:**
```bash
curl -X POST https://hc-ping.com/your-uuid
# Should return: OK
```

**Check network access:**
```bash
# Test from container
docker exec app curl -v https://hc-ping.com/your-uuid
```

**Verify timeout:**
```yaml
heartbeat:
  timeout: 30  # Increase if network is slow
```

### False Alerts

**Problem:** Getting alerts but task is running

**Cause 1: Timeout too short**
```yaml
heartbeat:
  timeout: 60  # Was 10, too short for slow network
```

**Cause 2: Grace period too short**
- Adjust in monitoring service dashboard
- Set grace period = (task duration × 2) + network time

**Cause 3: Retry failures**
```yaml
heartbeat:
  retry_count: 5  # Was 3, increase for flaky networks
  retry_delay: 10  # Was 5, give more time between retries
```

### Ping Succeeds but Task Failed

**Problem:** Task fails but success ping sent

**Check:** Verify script exits with proper code
```bash
#!/bin/bash
set -e  # Exit on any error

php artisan backup:run || exit 1  # Explicit exit on failure
```

## Best Practices

### ✅ Do

**Use heartbeats for critical tasks:**
```yaml
critical-backup:
  heartbeat:
    success_url: https://hc-ping.com/uuid  # Required!
```

**Set realistic timeouts:**
```yaml
heartbeat:
  timeout: 600  # Match or exceed task duration
```

**Use failure-only pings for high-frequency tasks:**
```yaml
health-pinger:
  schedule: "*/5 * * * *"  # Every 5 minutes
  heartbeat:
    failure_url: https://hc-ping.com/uuid/fail  # Only ping on failure
    # No success_url - reduces ping volume
```

**Add retry for network resilience:**
```yaml
heartbeat:
  retry_count: 3
  retry_delay: 5
```

### ❌ Don't

**Don't use heartbeats for every task:**
```yaml
# ❌ Overkill
trivial-task:
  schedule: "* * * * *"  # Every minute
  heartbeat: ...  # Unnecessary for non-critical frequent tasks
```

**Don't set timeout too low:**
```yaml
# ❌ Bad
long-backup:
  schedule: "0 2 * * *"
  heartbeat:
    timeout: 10  # Backup takes 20 minutes!

# ✅ Good
long-backup:
  heartbeat:
    timeout: 1800  # 30 minutes
```

**Don't forget to test:**
```bash
# ❌ Never tested heartbeat
# ✅ Test manually first
curl -X POST https://hc-ping.com/uuid
```

## Integration Examples

### Slack Notifications

**Via healthchecks.io:**
1. Add Slack integration in healthchecks.io
2. Configure channel: `#production-alerts`
3. Set alert policy: "Alert on first failure"

**Slack message:**
```
🚨 Critical Alert: database-backup failed
Task: database-backup
Exit code: 1
Last successful run: 24 hours ago
View details: https://healthchecks.io/checks/uuid
```

### PagerDuty Escalation

**Via Cronitor:**
1. Add PagerDuty integration
2. Configure escalation policy
3. Set severity: Critical → Page immediately

**Escalation:**
```
1. Task fails → Ping failure URL
2. Cronitor detects failure
3. PagerDuty incident created
4. On-call engineer paged
5. If not acknowledged → Escalate to manager
```

### Multi-Channel Alerts

**Via custom endpoint:**
```yaml
heartbeat:
  failure_url: https://alerts.example.com/task-failed
  headers:
    X-Alert-Channels: "slack,email,pagerduty"
    X-Severity: high
```

**Endpoint routes to multiple channels:**
```php
// Handle failure alert
Route::post('/task-failed', function (Request $request) {
    $task = $request->input('task');
    $exitCode = $request->input('exit_code');

    // Send Slack notification
    Slack::send("#alerts", "Task {$task} failed (exit: {$exitCode})");

    // Send email
    Mail::to('sre@example.com')->send(new TaskFailedAlert($task, $exitCode));

    // Create PagerDuty incident (if critical)
    if ($request->header('X-Severity') === 'critical') {
        PagerDuty::trigger("Task {$task} failed");
    }

    return response()->json(['status' => 'alerted']);
});
```

## Complete Configuration Examples

### Production Backup Job

```yaml
database-backup:
  command: ["php", "artisan", "backup:critical"]
  schedule: "0 3 * * *"  # Daily at 3 AM
  restart: never
  env:
    BACKUP_TYPE: full
    S3_BUCKET: production-backups
  heartbeat:
    success_url: https://hc-ping.com/backup-uuid
    failure_url: https://hc-ping.com/backup-uuid/fail
    timeout: 600  # 10-minute timeout
    retry_count: 3
    retry_delay: 10
    method: POST
    headers:
      X-Environment: production
      X-Backup-Type: critical
```

**healthchecks.io Configuration:**
- Schedule: Daily (every 1 day)
- Grace time: 30 minutes (task duration + buffer)
- Alert after: 1 missed ping
- Integrations: Slack (#production-alerts), Email (team@example.com), PagerDuty

### Data Sync with Multiple Monitoring

```yaml
data-sync:
  command: ["/sync-external-api.sh"]
  schedule: "*/30 * * * *"  # Every 30 minutes
  restart: never
  heartbeat:
    success_url: https://hc-ping.com/sync-uuid
    failure_url: https://cronitor.link/p/key/sync-job/fail  # Different service for failures
    timeout: 120
    retry_count: 5
```

### Weekly Maintenance

```yaml
weekly-maintenance:
  command: ["/weekly-tasks.sh"]
  schedule: "0 3 * * 0"  # Sunday 3 AM
  restart: never
  heartbeat:
    success_url: https://betteruptime.com/api/v1/heartbeat/weekly-uuid
    timeout: 1800  # 30 minutes for maintenance tasks
    headers:
      Authorization: Bearer ${BETTERUPTIME_TOKEN}
```

## Monitoring Dashboards

### healthchecks.io Dashboard

```
Your Checks:

✅ database-backup (UUID: abc123)
   Last ping: 2 hours ago
   Status: Up
   Grace: 30 minutes
   Integrations: Slack, Email

✅ api-sync (UUID: def456)
   Last ping: 15 minutes ago
   Status: Up
   Grace: 45 minutes

❌ weekly-report (UUID: ghi789)
   Last ping: 25 hours ago
   Status: DOWN - Expected ping 1 hour ago
   Alerts sent: Slack (#alerts), Email (3)
```

### Cronitor Dashboard

```
Monitors:

✅ backup-job
   Status: Passing
   Last run: 2024-11-21 03:00:00
   Duration: 8.5 minutes
   Success rate: 100% (30/30)

⚠️ data-sync
   Status: Warning
   Last run: 2024-11-21 10:30:15
   Duration: 2.1 minutes (usually 0.5 min - SLOW)
   Success rate: 93% (28/30)
```

## Metrics Integration

### Prometheus Metrics

Cbox Init exports heartbeat metrics:

```bash
# Heartbeat ping success
cbox_init_heartbeat_pings_total{task="backup-job",status="success"}

# Heartbeat ping failures
cbox_init_heartbeat_pings_total{task="backup-job",status="failure"}

# Last heartbeat timestamp
cbox_init_heartbeat_last_ping_timestamp{task="backup-job"}
```

### Alert on Heartbeat Failures

```yaml
# Prometheus alert
groups:
  - name: heartbeat_monitoring
    rules:
      - alert: HeartbeatPingFailing
        expr: |
          sum(rate(cbox_init_heartbeat_pings_total{status="failure"}[5m])) > 0
        labels:
          severity: warning
        annotations:
          summary: "Heartbeat ping failing for {{ $labels.task }}"

      - alert: HeartbeatNotSent
        expr: |
          time() - cbox_init_heartbeat_last_ping_timestamp > 3600
        labels:
          severity: critical
        annotations:
          summary: "No heartbeat from {{ $labels.task }} in 1 hour"
```

## Troubleshooting

### Ping URL Not Working

**Test manually:**
```bash
# Test success ping
curl -X POST https://hc-ping.com/uuid
# Expected: OK

# Test failure ping
curl -X POST "https://hc-ping.com/uuid/fail?exitCode=1"
# Expected: OK
```

**Check DNS resolution:**
```bash
docker exec app nslookup hc-ping.com
```

**Check network connectivity:**
```bash
docker exec app curl -v https://hc-ping.com/uuid
```

### Getting False Alerts

**Cause 1: Grace period too short**
- Increase grace period in monitoring service
- Grace = Task duration + Network time + Buffer

**Cause 2: Task timing varies**
```yaml
# If task takes 5-15 minutes
heartbeat:
  timeout: 1200  # 20 minutes (max duration + buffer)
```

**Cause 3: Network timeouts**
```yaml
heartbeat:
  retry_count: 5  # Retry more times
  retry_delay: 10  # Wait longer between retries
```

### Heartbeat Sent but Not Recorded

**Check service status:**
- Visit monitoring service status page
- Verify service is operational

**Check rate limits:**
- Some services limit ping frequency
- Check service documentation

**Verify UUID:**
```bash
# Ensure UUID is correct
echo $HEARTBEAT_UUID
curl -X POST "https://hc-ping.com/$HEARTBEAT_UUID"
```

## Best Practices

### ✅ Do

**Use for critical tasks:**
```yaml
critical-backup:
  heartbeat: ...  # Required

important-sync:
  heartbeat: ...  # Required

cache-warmer:
  # No heartbeat needed - not critical
```

**Set appropriate grace periods:**
- Daily task with 5-min duration → Grace: 60 minutes
- Hourly task with 30-sec duration → Grace: 10 minutes
- High-frequency task (every 5 min) → Grace: 15 minutes

**Test before production:**
```bash
# Manual test
CBOX_INIT_PROCESS_NAME=test-task \
  curl -X POST https://hc-ping.com/uuid
```

**Monitor heartbeat metrics:**
```promql
sum(rate(cbox_init_heartbeat_pings_total{status="failure"}[1h]))
```

### ❌ Don't

**Don't use for high-frequency tasks:**
```yaml
# ❌ Bad - every minute is too frequent
every-minute-task:
  schedule: "* * * * *"
  heartbeat: ...  # Too many pings

# ✅ Good - use failure-only
every-minute-task:
  heartbeat:
    failure_url: https://hc-ping.com/uuid/fail  # Only on failure
```

**Don't share heartbeat URLs:**
```yaml
# ❌ Bad - same URL for different tasks
backup-db:
  heartbeat:
    url: https://hc-ping.com/same-uuid

backup-files:
  heartbeat:
    url: https://hc-ping.com/same-uuid  # Can't distinguish!

# ✅ Good - unique URL per task
backup-db:
  heartbeat:
    url: https://hc-ping.com/db-backup-uuid

backup-files:
  heartbeat:
    url: https://hc-ping.com/files-backup-uuid
```

## See Also

- [Scheduled Tasks](scheduled-tasks) - Task scheduling
- [Process Configuration](../configuration/processes) - Heartbeat configuration
- [Examples](../examples/scheduled-tasks) - Practical heartbeat examples
- [Prometheus Metrics](../observability/metrics) - Heartbeat metrics

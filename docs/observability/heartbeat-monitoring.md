---
title: "Heartbeat Monitoring"
description: "Planned dead-man's-switch integration for scheduled tasks — not yet implemented"
weight: 45
---

# Heartbeat Monitoring

> **Status: Planned / not yet implemented.**
>
> Outbound heartbeat pings (dead-man's-switch integration with services such as
> healthchecks.io, Cronitor, or Better Uptime) are **not wired up** in the
> current release. The scheduler does not ping any external URL when a task
> runs, succeeds, or fails, and there are no heartbeat Prometheus metrics.

## What exists today

The configuration parser accepts a `heartbeat` block on a process for
forward-compatibility, but **no runtime code reads it** — setting it has no
effect. Only three fields are defined:

```yaml
processes:
  critical-backup:
    command: ["php", "artisan", "backup:critical"]
    schedule: "0 3 * * *"
    heartbeat:
      enabled: false   # accepted but ignored
      interval: 0      # accepted but ignored
      grace: 0         # accepted but ignored
```

There is no `url`, `success_url`, `failure_url`, `method`, `headers`,
`retry_count`, or `retry_delay` field — earlier drafts of this page documented
those, but they were never implemented.

## What to use instead today

Until heartbeats land, use one of these approaches:

- **Ping from inside the task.** Have your task script `curl` your monitoring
  service on success/failure. This works with any provider and gives you full
  control over payloads and exit-code handling:

  ```bash
  #!/bin/bash
  set -e
  php artisan backup:run
  curl -fsS -m 10 --retry 3 https://hc-ping.com/your-uuid-here
  ```

- **Poll the scheduler over the API.** Cbox Init records real per-task
  execution history and statistics, exposed at
  `GET /api/v1/processes/{name}/schedule` and
  `GET /api/v1/processes/{name}/schedule/history`. An external check can read
  the last run time, exit code, and success rate. See
  [Scheduled Tasks](../features/scheduled-tasks) for the response shape.

## See Also

- [Scheduled Tasks](../features/scheduled-tasks) - Cron scheduler and the schedule status API
- [Process Configuration](../configuration/processes) - Process settings
- [Prometheus Metrics](metrics) - Metrics that are actually exported

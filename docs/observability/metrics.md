---
title: "Prometheus Metrics"
description: "Monitor Cbox Init processes with comprehensive Prometheus metrics and alerting"
weight: 40
---

# Prometheus Metrics

Comprehensive Prometheus metrics for monitoring Cbox Init and managed processes.

## Configuration

Enable metrics in your `cbox-init.yaml`:

```yaml
global:
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: /metrics
```

## Available Metrics

### Process Lifecycle Metrics

#### `cbox_init_process_up`
**Type:** Gauge
**Labels:** `name`, `instance`
**Description:** Process status (1=running, 0=stopped)

```promql
# Query running instances of php-fpm
cbox_init_process_up{name="php-fpm"}
```

#### `cbox_init_process_restarts_total`
**Type:** Counter
**Labels:** `name`, `reason`
**Description:** Total number of process restarts by reason (crash, health_check, normal_exit)

```promql
# Total restarts for all processes
sum(cbox_init_process_restarts_total) by (name)

# Restarts due to health check failures
cbox_init_process_restarts_total{reason="health_check"}
```

#### `cbox_init_process_start_time_seconds`
**Type:** Gauge
**Labels:** `name`, `instance`
**Description:** Unix timestamp when process instance started

```promql
# Process uptime in seconds
time() - cbox_init_process_start_time_seconds
```

#### `cbox_init_process_last_exit_code`
**Type:** Gauge
**Labels:** `name`, `instance`
**Description:** Last exit code of process instance

```promql
# Non-zero exit codes (errors)
cbox_init_process_last_exit_code != 0
```

### Health Check Metrics

#### `cbox_init_health_check_status`
**Type:** Gauge
**Labels:** `name`, `type`
**Description:** Health check status (1=healthy, 0=unhealthy)

```promql
# Unhealthy processes
cbox_init_health_check_status == 0
```

#### `cbox_init_health_check_duration_seconds`
**Type:** Histogram
**Labels:** `name`, `type`
**Description:** Health check duration in seconds

```promql
# 95th percentile health check latency
histogram_quantile(0.95,
  sum(rate(cbox_init_health_check_duration_seconds_bucket[5m])) by (le, name)
)
```

#### `cbox_init_health_check_total`
**Type:** Counter
**Labels:** `name`, `type`, `status`
**Description:** Total number of health checks performed

```promql
# Health check failure rate
rate(cbox_init_health_check_total{status="failure"}[5m])
```

#### `cbox_init_health_check_consecutive_fails`
**Type:** Gauge
**Labels:** `name`
**Description:** Current consecutive health check failures

```promql
# Processes with multiple consecutive failures
cbox_init_health_check_consecutive_fails > 1
```

### Scaling Metrics

#### `cbox_init_process_desired_scale`
**Type:** Gauge
**Labels:** `name`
**Description:** Desired number of process instances

```promql
# Desired scale configuration
cbox_init_process_desired_scale
```

#### `cbox_init_process_current_scale`
**Type:** Gauge
**Labels:** `name`
**Description:** Current number of running instances

```promql
# Scale drift (actual vs desired)
cbox_init_process_current_scale - cbox_init_process_desired_scale
```

### Hook Execution Metrics

#### `cbox_init_hook_executions_total`
**Type:** Counter
**Labels:** `name`, `type`, `status`
**Description:** Total hook executions by type and status

```promql
# Failed pre-start hooks
cbox_init_hook_executions_total{type="pre_start", status="failure"}
```

#### `cbox_init_hook_duration_seconds`
**Type:** Histogram
**Labels:** `name`, `type`
**Description:** Hook execution duration in seconds

```promql
# 99th percentile hook duration
histogram_quantile(0.99,
  sum(rate(cbox_init_hook_duration_seconds_bucket[5m])) by (le, type)
)
```

### Manager Metrics

#### `cbox_init_manager_process_count`
**Type:** Gauge
**Description:** Total number of managed processes

```promql
# Total processes under management
cbox_init_manager_process_count
```

#### `cbox_init_manager_start_time_seconds`
**Type:** Gauge
**Description:** Unix timestamp when manager started

```promql
# Manager uptime in seconds
time() - cbox_init_manager_start_time_seconds
```

#### `cbox_init_build_info`
**Type:** Gauge
**Labels:** `version`, `go_version`
**Description:** Cbox Init build information

```promql
# Version information
cbox_init_build_info
```

## Common Queries

### Process Health Overview

```promql
# Count of healthy processes
sum(cbox_init_process_up) by (name)

# Count of processes with health check failures
count(cbox_init_health_check_status{status="0"}) by (name)
```

### Restart Monitoring

```promql
# Restart rate per minute
rate(cbox_init_process_restarts_total[1m])

# Processes restarting frequently (>5/hour)
sum(increase(cbox_init_process_restarts_total[1h])) by (name) > 5
```

### Scale Monitoring

```promql
# Instances not matching desired scale
abs(cbox_init_process_current_scale - cbox_init_process_desired_scale) > 0
```

### Hook Performance

```promql
# Slow hooks (>30s)
max(cbox_init_hook_duration_seconds) by (name, type) > 30

# Hook failure rate
rate(cbox_init_hook_executions_total{status="failure"}[5m])
```

## Alerting Rules

### Recommended Prometheus Alerts

```yaml
groups:
  - name: cbox_init
    rules:
      # Process down
      - alert: ProcessDown
        expr: cbox_init_process_up == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Process {{ $labels.name }} instance {{ $labels.instance }} is down"

      # Frequent restarts
      - alert: FrequentRestarts
        expr: rate(cbox_init_process_restarts_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Process {{ $labels.name }} restarting frequently"

      # Health check failures
      - alert: HealthCheckFailing
        expr: cbox_init_health_check_status == 0
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Health check failing for {{ $labels.name }}"

      # Scale drift
      - alert: ScaleDrift
        expr: abs(cbox_init_process_current_scale - cbox_init_process_desired_scale) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.name }} scale drift detected"

      # Hook failures
      - alert: HookFailures
        expr: rate(cbox_init_hook_executions_total{status="failure"}[5m]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Hook {{ $labels.name }} failing"
```

## Grafana Dashboard

### Sample Dashboard JSON

```json
{
  "dashboard": {
    "title": "Cbox Init Overview",
    "panels": [
      {
        "title": "Process Status",
        "targets": [
          {
            "expr": "cbox_init_process_up"
          }
        ]
      },
      {
        "title": "Restart Rate",
        "targets": [
          {
            "expr": "rate(cbox_init_process_restarts_total[5m])"
          }
        ]
      },
      {
        "title": "Health Check Status",
        "targets": [
          {
            "expr": "cbox_init_health_check_status"
          }
        ]
      },
      {
        "title": "Scale Status",
        "targets": [
          {
            "expr": "cbox_init_process_current_scale",
            "legendFormat": "Current"
          },
          {
            "expr": "cbox_init_process_desired_scale",
            "legendFormat": "Desired"
          }
        ]
      }
    ]
  }
}
```

## Scraping Configuration

### Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'cbox-init'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 15s
```

### Docker Compose Integration

```yaml
services:
  cbox-init:
    image: cboxdk/init:latest
    environment:
      - CBOX_INIT_GLOBAL_METRICS_ENABLED=true
      - CBOX_INIT_GLOBAL_METRICS_PORT=9090
    ports:
      - "9090:9090"

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    ports:
      - "9091:9090"
```

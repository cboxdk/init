---
title: "Prometheus Metrics"
description: "Monitor Cbox Init processes with comprehensive Prometheus metrics and alerting"
weight: 41
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

## One Endpoint: Federation and Embedded Engines

Since 3.2.0 the main `/metrics` response can carry the whole container's
story, so Prometheus scrapes one port:

- **Embedded engines merge natively.** The runtime PHP-FPM autotuner's
  `fpm_tune_*` series always appear on the main endpoint when `fpm_tune` is
  enabled. `fpm_tune.metrics_addr` remains optional, for parity with the
  standalone tool.
- **Local exporters federate.** Declare them under
  `global.metrics_federate` and their exposition is appended to every
  scrape, with a short cache so heavy scraping does not multiply load:

```yaml
global:
  metrics_federate:
    - name: fpm-exporter
      url: http://127.0.0.1:9114/metrics
      timeout: 2s     # per-fetch (default)
      cache_ttl: 5s   # cached between fetches (default)
```

Each source contributes a health gauge; a source that is down degrades
instead of failing the scrape:

```text
cbox_init_federate_up{name="fpm-exporter"} 1
```

Rules and caveats:

- URLs must point at **loopback** (`127.0.0.1`, `::1`, `localhost`) —
  federation merges exporters inside the container; it is not a proxy, and
  config validation rejects anything else.
- Bodies over 8 MiB and non-200 responses count as down.
- Metric names must not collide across sources — federation appends
  expositions verbatim and does not rewrite names.
- With federation enabled the endpoint always serves the plain-text
  exposition format (no content negotiation), because federated bodies are
  appended as-is.

A complete example lives in `configs/examples/metrics-federate.yaml`.

## Available Metrics

### Label Reference

Every series Cbox Init exposes under the `cbox_init_` prefix, with its exact
label set. Selectors must use these keys: a selector on a label a metric does
not carry matches nothing.

| Metric | Type | Labels |
|--------|------|--------|
| `cbox_init_process_up` | Gauge | `name`, `instance` |
| `cbox_init_process_start_time_seconds` | Gauge | `name`, `instance` |
| `cbox_init_process_last_exit_code` | Gauge | `name`, `instance` |
| `cbox_init_process_restarts_total` | Counter | `name`, `reason` |
| `cbox_init_process_desired_scale` | Gauge | `name` |
| `cbox_init_process_current_scale` | Gauge | `name` |
| `cbox_init_health_check_status` | Gauge | `name`, `type` |
| `cbox_init_health_check_duration_seconds` | Histogram | `name`, `type` |
| `cbox_init_health_check_total` | Counter | `name`, `type`, `status` |
| `cbox_init_health_check_consecutive_fails` | Gauge | `name` |
| `cbox_init_process_cpu_percent` | Gauge | `process`, `instance` |
| `cbox_init_process_memory_bytes` | Gauge | `process`, `instance`, `type` |
| `cbox_init_process_memory_percent` | Gauge | `process`, `instance` |
| `cbox_init_process_threads` | Gauge | `process`, `instance` |
| `cbox_init_process_file_descriptors` | Gauge | `process`, `instance` |
| `cbox_init_resource_collection_errors_total` | Counter | `process`, `instance` |
| `cbox_init_resource_collection_duration_seconds` | Histogram | none |
| `cbox_init_hook_executions_total` | Counter | `name`, `type`, `status` |
| `cbox_init_hook_duration_seconds` | Histogram | `name`, `type` |
| `cbox_init_manager_process_count` | Gauge | none |
| `cbox_init_manager_start_time_seconds` | Gauge | none |
| `cbox_init_shutdown_duration_seconds` | Histogram | none |
| `cbox_init_build_info` | Gauge | `version`, `go_version` |
| `cbox_init_federate_up` | Gauge | `name` |

Label values:

- **`name`** is the process name from the config (`php-fpm`), except on the
  hook metrics, where it is the hook's name, and on
  `cbox_init_federate_up`, where it is the federated source's name.
- **`process`** is the process name from the config. Only the resource
  metrics use it.
- **`instance`** is the instance ID, `<process>-<index>` counting from 0
  (`php-fpm-0`, `php-fpm-1`, ...). See the note below on how Prometheus
  stores it.
- **`reason`** (restarts): `crash`, `normal_exit`, `health_check`,
  `memory_limit`.
- **`type`**: the health check type (`tcp`, `http`, `exec`) on health check
  metrics, the hook type (`pre_start`, `post_start`, `pre_stop`,
  `post_stop`) on hook metrics, and `rss` or `vms` on
  `cbox_init_process_memory_bytes`.
- **`status`**: `success` or `failure`.

#### `name` vs `process`

The process name sits under **`name`** on the lifecycle, health check and
scaling metrics, and under **`process`** on the resource metrics. The two
sets are not interchangeable: `cbox_init_process_up{process="php-fpm"}`
returns nothing. To combine them, rename one side with `label_replace`:

```promql
# CPU of instances that are currently up
cbox_init_process_cpu_percent
  and on (instance, exported_instance, process)
label_replace(cbox_init_process_up == 1, "process", "$1", "name", "(.*)")
```

#### `instance` and `exported_instance`

Prometheus attaches its own `instance` label (the scrape target, e.g.
`app:9090`) to every series. With the default `honor_labels: false`, a
conflicting `instance` label in the scraped data is renamed to
`exported_instance`. So once scraped, the instance ID above is in
`exported_instance` and `instance` is the target:

```promql
# One instance of one container
cbox_init_process_up{instance="app:9090", name="php-fpm", exported_instance="php-fpm-0"}
```

The examples on this page use `exported_instance` in PromQL. With
`honor_labels: true` the instance ID stays in `instance` instead, and the
scrape target's `instance` is dropped for these series.

#### Series lifecycle

- A **stopped or crashed** instance keeps its series, with
  `cbox_init_process_up` at `0`. That is the signal to alert on.
- An instance **removed by a scale-down** has all of its per-instance series
  deleted (`cbox_init_process_up`, `_start_time_seconds`,
  `_last_exit_code`, and every resource metric), so
  `cbox_init_process_up == 0` does not keep firing for an instance that was
  scaled away on purpose. The process-level series (`restarts_total`,
  `desired_scale`, `current_scale`, health checks) stay.

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
**Description:** Total number of process restarts by reason (`crash`, `normal_exit`, `health_check`, `memory_limit`)

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

### Resource Metrics

Exposed when `resource_metrics_enabled` and `metrics_enabled` are both true.
These use **`process`**, not `name`, for the process name. Collection
details and more queries: [Resource Monitoring](resource-monitoring).

#### `cbox_init_process_cpu_percent`
**Type:** Gauge
**Labels:** `process`, `instance`
**Description:** CPU usage in percent of one core (can exceed 100 on multi-core)

```promql
# Average CPU across php-fpm instances
avg(cbox_init_process_cpu_percent{process="php-fpm"})
```

#### `cbox_init_process_memory_bytes`
**Type:** Gauge
**Labels:** `process`, `instance`, `type` (`rss`, `vms`)
**Description:** Memory usage in bytes

```promql
# Resident memory per process
sum(cbox_init_process_memory_bytes{type="rss"}) by (process)
```

#### `cbox_init_process_memory_percent`
**Type:** Gauge
**Labels:** `process`, `instance`
**Description:** Memory usage as a percentage of total system memory

#### `cbox_init_process_threads`
**Type:** Gauge
**Labels:** `process`, `instance`
**Description:** Number of threads in the process

#### `cbox_init_process_file_descriptors`
**Type:** Gauge
**Labels:** `process`, `instance`
**Description:** Open file descriptors (Linux only; absent where unavailable)

#### `cbox_init_resource_collection_errors_total`
**Type:** Counter
**Labels:** `process`, `instance`
**Description:** Failed resource samples

#### `cbox_init_resource_collection_duration_seconds`
**Type:** Histogram
**Labels:** none
**Description:** Time taken by one resource collection pass

### Hook Execution Metrics

#### `cbox_init_hook_executions_total`
**Type:** Counter
**Labels:** `name` (hook name), `type`, `status`
**Description:** Total hook executions by type and status

```promql
# Failed pre-start hooks
cbox_init_hook_executions_total{type="pre_start", status="failure"}
```

#### `cbox_init_hook_duration_seconds`
**Type:** Histogram
**Labels:** `name` (hook name), `type`
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

#### `cbox_init_shutdown_duration_seconds`
**Type:** Histogram
**Labels:** none
**Description:** Duration of graceful shutdown in seconds

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
# Running instances per process
sum(cbox_init_process_up) by (name)

# Count of processes with health check failures
count(cbox_init_health_check_status == 0) by (name)
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
          summary: "Process {{ $labels.name }} ({{ $labels.exported_instance }}) on {{ $labels.instance }} is down"

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

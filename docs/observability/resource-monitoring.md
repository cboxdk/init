---
title: "Resource Monitoring"
description: "Track CPU, memory, threads, and file descriptors with time series storage and REST API queries"
weight: 43
---

# Resource Monitoring

Cbox Init tracks detailed resource usage (CPU, memory, threads, file descriptors) for all managed process instances. Historical data is stored in time series ring buffers and exposed via both REST API and Prometheus metrics.

## Overview

**Features:**
- 📊 Per-instance resource tracking
- ⏱️ Configurable collection interval
- 💾 Time series ring buffer with history
- 🔌 REST API for querying historical data
- 📈 Prometheus exposition for dashboards
- 🎯 Zero overhead when disabled

## Quick Start

**Enable resource monitoring:**

```yaml
global:
  # Enable resource tracking
  resource_metrics_enabled: true
  resource_metrics_interval: 5       # Collection interval in seconds
  resource_metrics_max_samples: 720  # Max samples per instance (1 hour at 5s)

  # Enable Prometheus export (optional)
  metrics_enabled: true
  metrics_port: 9090
```

**Query via REST API:**

```bash
# Get last 20 samples for php-fpm instance 0
curl "http://localhost:9180/api/v1/metrics/history?process=php-fpm&instance=php-fpm-0&limit=20"
```

**Query via Prometheus:**

```bash
# View all resource metrics
curl http://localhost:9090/metrics | grep cbox_init_process
```

## Configuration

### Resource Metrics Settings

```yaml
global:
  # Enable/disable resource tracking
  resource_metrics_enabled: true     # Default: false

  # Collection interval in seconds
  resource_metrics_interval: 5       # Default: 5, Range: 1-300

  # Maximum samples per instance (ring buffer size)
  resource_metrics_max_samples: 720  # Default: 720 (1 hour at 5s interval)

  # Enable Prometheus HTTP server
  metrics_enabled: true              # Default: false
  metrics_port: 9090                 # Default: 9090
  metrics_path: /metrics             # Default: /metrics
```

### Memory Usage Calculation

**Per-instance ring buffer memory:**
```
Sample size: ~96 bytes
Total memory = sample_size × max_samples × instance_count

Example:
720 samples × 96 bytes × 10 instances = ~691 KB
```

**Recommended limits:**
- Development: 360 samples (30 min at 5s interval) = ~346 KB per 10 instances
- Production: 720 samples (1 hour at 5s interval) = ~691 KB per 10 instances
- High-scale: 1440 samples (2 hours at 5s interval) = ~1.38 MB per 10 instances

## Collected Metrics

### CPU Usage

**`cpu_percent`** - CPU usage percentage

- **Unit:** Percent (0-100 per core)
- **Meaning:** Total CPU time consumed by process
- **Can exceed 100%:** Yes (multi-threaded processes on multi-core systems)
- **Example:** 250% = using 2.5 cores fully

### Memory Usage

**`memory_rss_bytes`** - Resident Set Size (Physical Memory)

- **Unit:** Bytes
- **Meaning:** Physical RAM currently used by process
- **Includes:** Code, data, shared libraries actively in memory
- **Most useful for:** Detecting memory leaks

**`memory_vms_bytes`** - Virtual Memory Size

- **Unit:** Bytes
- **Meaning:** Total virtual memory allocated (may not be resident)
- **Includes:** All memory mappings, swap, etc.
- **Note:** Can be much larger than RSS

**`memory_percent`** - Memory as % of Total System Memory

- **Unit:** Percent (0-100)
- **Meaning:** RSS as percentage of total system RAM
- **Useful for:** Capacity planning

### Threads

**`threads`** - Number of Threads

- **Unit:** Count
- **Meaning:** Total threads (or processes if not multi-threaded)
- **For PHP-FPM:** Shows number of child processes
- **For multi-threaded apps:** Shows all threads

### File Descriptors

**`file_descriptors`** - Open File Descriptors

- **Unit:** Count
- **Meaning:** Number of open files, sockets, pipes
- **Platform:** Linux only (not available on macOS)
- **Useful for:** Detecting fd leaks

## REST API

### Query Historical Metrics

**Endpoint:** `GET /api/v1/metrics/history`

**Parameters:**

| Parameter | Required | Description | Default |
|-----------|----------|-------------|---------|
| `process` | ✅ Yes | Process name | - |
| `instance` | ✅ Yes | Instance ID (e.g., `php-fpm-0`) | - |
| `since` | No | Start time (RFC3339 or Unix timestamp) | 1 hour ago |
| `limit` | No | Max samples to return (1-10000) | 100 |

**Example requests:**

```bash
# Get last 20 samples
curl "http://localhost:9180/api/v1/metrics/history?process=php-fpm&instance=php-fpm-0&limit=20"

# Get samples since specific time (RFC3339)
curl "http://localhost:9180/api/v1/metrics/history?process=nginx&instance=nginx-0&since=2025-11-23T08:00:00Z&limit=50"

# Get samples since Unix timestamp
curl "http://localhost:9180/api/v1/metrics/history?process=horizon&instance=horizon-0&since=1732348800&limit=100"

# With authentication
curl -H "Authorization: Bearer your-token" \
  "http://localhost:9180/api/v1/metrics/history?process=queue-worker&instance=queue-worker-0&limit=10"
```

### Response Format

```json
{
  "process": "php-fpm",
  "instance": "php-fpm-0",
  "since": "2025-11-23T08:00:00Z",
  "limit": 20,
  "samples": 20,
  "data": [
    {
      "timestamp": "2025-11-23T09:27:02.263Z",
      "cpu_percent": 12.5,
      "memory_rss_bytes": 134217728,
      "memory_vms_bytes": 445747003392,
      "memory_percent": 1.95,
      "threads": 8,
      "file_descriptors": 42
    },
    {
      "timestamp": "2025-11-23T09:27:07.315Z",
      "cpu_percent": 15.2,
      "memory_rss_bytes": 135266304,
      "memory_vms_bytes": 445747003392,
      "memory_percent": 1.97,
      "threads": 8,
      "file_descriptors": 43
    }
  ]
}
```

**Field descriptions:**

- `process` - Process name from configuration
- `instance` - Instance ID (format: `{process}-{index}`)
- `since` - Start time for query (RFC3339 format)
- `limit` - Maximum samples requested
- `samples` - Actual number of samples returned
- `data` - Array of metric samples (chronological order)

### Error Responses

**404 Not Found:**
```json
{
  "error": "Process 'unknown' not found"
}
```

**400 Bad Request:**
```json
{
  "error": "Invalid 'since' parameter: must be RFC3339 or Unix timestamp"
}
```

**400 Bad Request:**
```json
{
  "error": "Invalid 'limit': must be between 1 and 10000"
}
```

## Prometheus Metrics

When both `resource_metrics_enabled` and `metrics_enabled` are true, resource metrics are exposed on the Prometheus endpoint.

### Metric Names

All metrics use the prefix `cbox_init_process_` and include labels:
- `process` - Process name
- `instance` - Instance ID

**Available metrics:**

```promql
# CPU usage (percentage, can exceed 100%)
cbox_init_process_cpu_percent{process="php-fpm", instance="php-fpm-0"}

# Memory RSS in bytes
cbox_init_process_memory_bytes{process="php-fpm", instance="php-fpm-0", type="rss"}

# Memory VMS in bytes
cbox_init_process_memory_bytes{process="php-fpm", instance="php-fpm-0", type="vms"}

# Memory as % of total system RAM
cbox_init_process_memory_percent{process="php-fpm", instance="php-fpm-0"}

# Number of threads/processes
cbox_init_process_threads{process="php-fpm", instance="php-fpm-0"}

# Open file descriptors (Linux only)
cbox_init_process_file_descriptors{process="php-fpm", instance="php-fpm-0"}
```

### Collection Metadata

**Error tracking:**
```promql
# Total collection errors per instance
cbox_init_resource_collection_errors_total{process="...", instance="..."}
```

**Performance:**
```promql
# Collection duration in seconds
cbox_init_resource_collection_duration_seconds
```

### Query Examples

**Average CPU across all PHP-FPM instances:**
```promql
avg(cbox_init_process_cpu_percent{process="php-fpm"})
```

**Memory usage trend for specific instance:**
```promql
cbox_init_process_memory_bytes{process="nginx", instance="nginx-0", type="rss"}
```

**Total threads across all processes:**
```promql
sum(cbox_init_process_threads)
```

**Top 5 processes by memory:**
```promql
topk(5, cbox_init_process_memory_bytes{type="rss"})
```

**Memory usage rate of change (MB/hour):**
```promql
rate(cbox_init_process_memory_bytes{type="rss"}[1h]) * 3600 / 1024 / 1024
```

## Grafana Integration

### Dashboard Setup

**1. Add Prometheus Data Source:**
- URL: `http://localhost:9090`
- Scrape interval: Match `resource_metrics_interval` (e.g., 5s)

**2. Create Dashboard Panels:**

### CPU Usage Panel

```json
{
  "title": "CPU Usage by Process",
  "targets": [
    {
      "expr": "cbox_init_process_cpu_percent",
      "legendFormat": "{{process}}-{{instance}}"
    }
  ],
  "yaxis": {
    "label": "CPU %",
    "format": "percent"
  }
}
```

### Memory Usage Panel

```json
{
  "title": "Memory Usage (RSS)",
  "targets": [
    {
      "expr": "cbox_init_process_memory_bytes{type=\"rss\"}",
      "legendFormat": "{{process}}-{{instance}}"
    }
  ],
  "yaxis": {
    "label": "Memory",
    "format": "bytes"
  }
}
```

### Thread Count Panel

```json
{
  "title": "Thread Count",
  "targets": [
    {
      "expr": "cbox_init_process_threads",
      "legendFormat": "{{process}}-{{instance}}"
    }
  ]
}
```

### File Descriptors Panel

```json
{
  "title": "Open File Descriptors",
  "targets": [
    {
      "expr": "cbox_init_process_file_descriptors",
      "legendFormat": "{{process}}-{{instance}}"
    }
  ]
}
```

### Alert Rules

**High CPU usage:**
```yaml
- alert: HighCPUUsage
  expr: cbox_init_process_cpu_percent > 80
  for: 5m
  annotations:
    summary: "High CPU usage detected"
    description: "{{$labels.process}}-{{$labels.instance}} using {{$value}}% CPU"
```

**Memory leak detection:**
```yaml
- alert: MemoryLeak
  expr: rate(cbox_init_process_memory_bytes{type="rss"}[1h]) > 10485760  # 10MB/hour
  for: 3h
  annotations:
    summary: "Potential memory leak"
    description: "{{$labels.process}}-{{$labels.instance}} growing {{$value | humanize}}B/s"
```

**File descriptor exhaustion:**
```yaml
- alert: FileDescriptorLeak
  expr: cbox_init_process_file_descriptors > 1000
  for: 5m
  annotations:
    summary: "High file descriptor count"
    description: "{{$labels.process}}-{{$labels.instance}} has {{$value}} open FDs"
```

## Performance Considerations

### CPU Overhead

**Collection cost:** ~1ms per process instance per collection cycle

**Example:**
- 10 instances × 1ms = 10ms per collection
- At 5s interval = 10ms / 5000ms = 0.2% CPU overhead
- Negligible for most workloads

### Memory Usage

**Ring buffer memory:**
- Each sample: ~96 bytes
- Per instance: `96 bytes × max_samples`
- Total: `96 bytes × max_samples × instance_count`

**Examples:**
- 720 samples × 10 instances = ~691 KB
- 1440 samples × 20 instances = ~2.7 MB
- 360 samples × 50 instances = ~1.7 MB

### Recommended Intervals

| Environment | Interval | Max Samples | History | Overhead |
|-------------|----------|-------------|---------|----------|
| **Development** | 2-5s | 360 | 30 min | Very low |
| **Production** | 10-30s | 720 | 2-6 hours | Low |
| **High-Scale** | 60s+ | 1440 | 24 hours | Minimal |

**Tuning guidelines:**
- Lower interval = more granular data, higher overhead
- Higher interval = less overhead, coarser granularity
- Adjust `max_samples` to control history retention

## Troubleshooting

### Metrics Not Collected

**Issue:** Resource metrics not appearing in API or Prometheus

**Solutions:**

1. **Check configuration:**
   ```yaml
   global:
     resource_metrics_enabled: true  # Must be true
     metrics_enabled: true            # For Prometheus
   ```

2. **Verify processes are running:**
   ```bash
   curl http://localhost:9180/api/v1/processes
   ```

3. **Check for collection errors:**
   ```bash
   # View Cbox logs
   journalctl -u cbox-init -f | grep "resource collection"
   ```

### Missing Prometheus Metrics

**Issue:** `cbox_init_process_*` metrics not in Prometheus

**Solutions:**

1. **Ensure both flags enabled:**
   ```yaml
   global:
     resource_metrics_enabled: true  # Collect metrics
     metrics_enabled: true            # Expose via HTTP
   ```

2. **Check Prometheus scrape config:**
   ```yaml
   scrape_configs:
     - job_name: 'cbox-init'
       static_configs:
         - targets: ['localhost:9090']
   ```

3. **Test endpoint manually:**
   ```bash
   curl http://localhost:9090/metrics | grep cbox_init_process
   ```

### File Descriptors Always Zero

**Issue:** `file_descriptors` metric is always 0

**Reason:** File descriptor tracking only available on Linux

**macOS behavior:**
- CPU, memory, threads: ✅ Available
- File descriptors: ❌ Not available (OS limitation)

### High Memory Usage

**Issue:** Cbox Init using more memory than expected

**Solutions:**

1. **Reduce max samples:**
   ```yaml
   global:
     resource_metrics_max_samples: 360  # Reduce from 720
   ```

2. **Increase interval:**
   ```yaml
   global:
     resource_metrics_interval: 15  # Increase from 5s
   ```

3. **Calculate expected memory:**
   ```
   Memory = 96 bytes × max_samples × instance_count
   ```

### Collection Errors in Logs

**Issue:** `Failed to collect resource metrics` in logs

**Causes:**
- Process PID no longer exists
- Permission denied (rare)
- System resource temporarily unavailable

**Solution:**
- Errors are logged but don't crash processes
- Graceful degradation - collection skipped for that cycle
- Check if processes are healthy with health checks

## Use Cases

### Memory Leak Detection

**Monitor memory growth over time:**

```bash
# Query last hour of data
curl "http://localhost:9180/api/v1/metrics/history?process=horizon&instance=horizon-0&limit=720" \
  | jq '.data[] | {timestamp, memory_rss_mb: (.memory_rss_bytes / 1024 / 1024)}'
```

**Grafana alert:**
```yaml
- alert: MemoryGrowth
  expr: rate(cbox_init_process_memory_bytes{type="rss"}[6h]) > 1048576  # 1MB/hour
  for: 12h
```

### Performance Profiling

**Identify CPU-intensive instances:**

```promql
# Top 3 CPU consumers
topk(3, avg_over_time(cbox_init_process_cpu_percent[5m]))
```

### Capacity Planning

**Aggregate resource usage:**

```promql
# Total memory across all instances
sum(cbox_init_process_memory_bytes{type="rss"})

# Average CPU across all processes
avg(cbox_init_process_cpu_percent)
```

### Thread Monitoring

**Detect thread pool exhaustion:**

```promql
# PHP-FPM child process count
cbox_init_process_threads{process="php-fpm"}
```

**Alert on low spare workers:**
```yaml
- alert: LowSpareWorkers
  expr: cbox_init_process_threads{process="php-fpm"} > 50  # Max children threshold
  for: 5m
```

## Next Steps

- [Prometheus Metrics](metrics) - Complete metrics documentation
- [Management API](api) - REST API reference
- [Examples](../examples/laravel-with-monitoring) - Real-world monitoring configuration

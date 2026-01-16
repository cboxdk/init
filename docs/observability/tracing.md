---
title: "Distributed Tracing"
description: "OpenTelemetry-based distributed tracing for deep observability into process lifecycle and operations"
weight: 44
---

# Distributed Tracing

Cbox Init supports distributed tracing using OpenTelemetry for deep observability into process lifecycle operations. Integrate with Jaeger, Grafana Tempo, Honeycomb, or any OpenTelemetry-compatible backend.

## Overview

**Features:**
- 🔭 **OpenTelemetry Protocol (OTLP)** - Industry-standard tracing
- 📡 **gRPC Export** - Efficient binary protocol
- 🔍 **Process Lifecycle Spans** - Trace start, stop, restart operations
- 🎯 **Contextual Attributes** - Process names, instance IDs, errors
- 📊 **Sampling Control** - Configurable sample rates
- 🚀 **Low Overhead** - <1ms per span with batched export

## Quick Start

**Enable tracing:**

```yaml
global:
  # Enable distributed tracing
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: localhost:4317
  tracing_sample_rate: 1.0
  tracing_service_name: cbox-init
```

**With Jaeger:**

```bash
# Start Jaeger all-in-one
docker run -d --name jaeger \
  -p 6831:6831/udp \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest

# Configure Cbox Init
global:
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: localhost:4317

# View traces: http://localhost:16686
```

## Configuration

### Tracing Settings

```yaml
global:
  # Enable/disable distributed tracing
  tracing_enabled: true              # Default: false

  # Exporter type
  tracing_exporter: otlp-grpc        # Options: otlp-grpc, stdout

  # Exporter endpoint
  tracing_endpoint: localhost:4317   # Default depends on exporter

  # Sampling rate (0.0-1.0)
  tracing_sample_rate: 1.0           # Default: 1.0 (100%)

  # Service name in traces
  tracing_service_name: cbox-init    # Default: cbox-init
```

### Exporter Types

#### 1. OTLP gRPC (Production)

**Best for:** Production deployments with Jaeger, Grafana Tempo, etc.

```yaml
global:
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: tempo:4317        # Grafana Tempo
  # OR
  tracing_endpoint: jaeger:4317       # Jaeger
  # OR
  tracing_endpoint: collector:4317    # OpenTelemetry Collector
```

**Advantages:**
- ✅ Efficient binary protocol (Protocol Buffers)
- ✅ Batched export for performance
- ✅ Industry-standard compatibility
- ✅ Low overhead (~0.5ms per span)

#### 2. Stdout (Development/Debugging)

**Best for:** Local development and debugging

```yaml
global:
  tracing_enabled: true
  tracing_exporter: stdout
  tracing_sample_rate: 1.0
```

**Output format:**
```json
{
  "Name": "process_manager.start_process",
  "SpanContext": {
    "TraceID": "4bf92f3577b34da6a3ce929d0e0e4736",
    "SpanID": "00f067aa0ba902b7",
    "TraceFlags": "01"
  },
  "Parent": {
    "TraceID": "4bf92f3577b34da6a3ce929d0e0e4736",
    "SpanID": "b5b4e0c8e1a2b3d4"
  },
  "SpanKind": "Internal",
  "StartTime": "2025-11-23T10:30:15.123456Z",
  "EndTime": "2025-11-23T10:30:15.234567Z",
  "Attributes": [
    {"Key": "process.name", "Value": {"Type": "STRING", "Value": "php-fpm"}},
    {"Key": "process.scale", "Value": {"Type": "INT64", "Value": 2}}
  ],
  "Status": {"Code": "Ok"}
}
```

**Advantages:**
- ✅ No external dependencies
- ✅ Immediate visibility
- ✅ Pretty-printed JSON
- ❌ Not suitable for production (high overhead)

### Sampling Rates

Control what percentage of operations are traced:

```yaml
global:
  tracing_sample_rate: 1.0   # 100% - trace everything (development)
  tracing_sample_rate: 0.1   # 10% - production sampling
  tracing_sample_rate: 0.01  # 1% - high-traffic production
```

**Guidelines:**
- **Development:** 1.0 (100%) - trace all operations
- **Staging:** 0.5 (50%) - balance coverage and overhead
- **Production (low traffic):** 0.1 (10%) - sufficient sampling
- **Production (high traffic):** 0.01 (1%) - minimize overhead

## Instrumented Operations

Cbox Init automatically creates spans for key process lifecycle operations:

### Process Manager Operations

#### 1. `process_manager.start` (Root Span)

**Triggered:** Overall Cbox Init startup

**Attributes:**
- `process.count` - Number of processes being started

**Example:**
```
process_manager.start [123.45ms]
  ├─ process_manager.start_process (php-fpm) [45.23ms]
  ├─ process_manager.start_process (nginx) [12.34ms]
  └─ process_manager.start_process (horizon) [65.88ms]
```

#### 2. `process_manager.start_process` (Child Span)

**Triggered:** Starting individual process

**Attributes:**
- `process.name` - Process name (e.g., "php-fpm")
- `process.scale` - Number of instances
- `error` - Error message (if failed)

**Example span:**
```json
{
  "name": "process_manager.start_process",
  "attributes": {
    "process.name": "php-fpm",
    "process.scale": 2
  },
  "duration_ms": 45.23,
  "status": "OK"
}
```

#### 3. `process_manager.shutdown` (Root Span)

**Triggered:** Graceful shutdown initiated

**Attributes:**
- `process.count` - Number of processes being stopped
- `shutdown.reason` - Why shutdown triggered (e.g., "SIGTERM", "user request")

**Example:**
```
process_manager.shutdown [3.5s]
  ├─ stop horizon [1.2s]
  ├─ stop queue-workers [0.8s]
  ├─ stop nginx [0.5s]
  └─ stop php-fpm [1.0s]
```

### Span Hierarchy

**Typical trace structure:**

```
process_manager.start (root)
│
├─ process_manager.start_process (php-fpm)
│  └─ [attributes: name=php-fpm, scale=2]
│
├─ process_manager.start_process (nginx)
│  └─ [attributes: name=nginx, scale=1, depends_on=[php-fpm]]
│
└─ process_manager.start_process (horizon)
   └─ [attributes: name=horizon, scale=1]
```

**On error:**
```
process_manager.start (root)
│
└─ process_manager.start_process (queue-worker)
   └─ [attributes: name=queue-worker, error="command not found: nonexistent"]
   └─ [status: ERROR]
```

## Integration Examples

### Jaeger

**Deploy Jaeger:**

```bash
# Docker (all-in-one)
docker run -d \
  --name jaeger \
  -p 6831:6831/udp \
  -p 16686:16686 \
  -p 4317:4317 \
  -p 4318:4318 \
  jaegertracing/all-in-one:latest
```

**Configure Cbox Init:**

```yaml
global:
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: localhost:4317
  tracing_sample_rate: 1.0
  tracing_service_name: cbox-init-production
```

**View traces:**
- Open: http://localhost:16686
- Service: `cbox-init-production`
- Operation: `process_manager.start`, `process_manager.start_process`

### Grafana Tempo

**Deploy Tempo:**

```yaml
# docker-compose.yml
services:
  tempo:
    image: grafana/tempo:latest
    ports:
      - "4317:4317"  # OTLP gRPC
      - "3200:3200"  # Tempo HTTP
    volumes:
      - ./tempo-config.yaml:/etc/tempo.yaml
    command: ["-config.file=/etc/tempo.yaml"]

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
      - GF_AUTH_ANONYMOUS_ORG_ROLE=Admin
```

**Configure Cbox Init:**

```yaml
global:
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: tempo:4317
  tracing_sample_rate: 0.1
  tracing_service_name: cbox-init
```

**Query in Grafana:**
- Add Tempo data source
- Explore → Tempo
- Search for service: `cbox-init`

### Honeycomb

**Configure Cbox Init:**

```yaml
global:
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: api.honeycomb.io:443
  tracing_sample_rate: 1.0
  tracing_service_name: cbox-init
```

**Set API key:**

```bash
# Via environment variable
export OTEL_EXPORTER_OTLP_HEADERS="x-honeycomb-team=YOUR_API_KEY"

# Run Cbox Init
./cbox-init
```

### OpenTelemetry Collector

**Deploy Collector:**

```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:

exporters:
  jaeger:
    endpoint: jaeger:14250
  tempo:
    endpoint: tempo:4317

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [jaeger, tempo]
```

**Configure Cbox Init:**

```yaml
global:
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: otel-collector:4317
  tracing_sample_rate: 1.0
```

## Docker Compose Example

**Complete stack with Jaeger:**

```yaml
version: '3.8'

services:
  app:
    build: .
    environment:
      - CBOX_INIT_GLOBAL_TRACING_ENABLED=true
      - CBOX_INIT_GLOBAL_TRACING_EXPORTER=otlp-grpc
      - CBOX_INIT_GLOBAL_TRACING_ENDPOINT=jaeger:4317
      - CBOX_INIT_GLOBAL_TRACING_SAMPLE_RATE=1.0
    depends_on:
      - jaeger

  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686"  # Jaeger UI
      - "4317:4317"    # OTLP gRPC
    environment:
      - COLLECTOR_OTLP_ENABLED=true
```

**Access Jaeger UI:** http://localhost:16686

## Use Cases

### 1. Startup Performance Analysis

**Trace process startup times:**

1. Enable tracing with 100% sampling
2. Start Cbox Init
3. Query for `process_manager.start` span
4. View child spans for each process
5. Identify slow-starting processes

**Example insights:**
- `php-fpm` starts in 45ms
- `nginx` starts in 12ms
- `horizon` starts in 66ms (slow - investigate)

### 2. Dependency Chain Visualization

**Understand startup order:**

1. View `process_manager.start` trace
2. Observe span hierarchy matching `depends_on`
3. Verify correct startup sequence
4. Identify parallelization opportunities

### 3. Error Debugging

**Trace failed process starts:**

1. Process fails to start
2. Query for spans with `status=ERROR`
3. View error attribute with failure reason
4. Correlate with logs using timestamps

**Example error span:**
```json
{
  "name": "process_manager.start_process",
  "attributes": {
    "process.name": "queue-worker",
    "process.scale": 3,
    "error": "exec: \"nonexistent\": executable file not found in $PATH"
  },
  "status": "ERROR"
}
```

### 4. Shutdown Analysis

**Trace graceful shutdown:**

1. Trigger shutdown (SIGTERM)
2. Query for `process_manager.shutdown` span
3. View shutdown duration per process
4. Identify processes slow to terminate
5. Adjust timeouts if needed

## Performance Impact

### Overhead Measurements

**With OTLP gRPC exporter:**
- Span creation: ~0.1ms
- Span export (batched): ~0.5ms per batch
- Total overhead: <1ms per operation
- Negligible for most workloads

**With stdout exporter:**
- Span creation: ~0.1ms
- Pretty-print JSON: ~5-10ms per span
- Total overhead: ~10ms per operation
- **Not recommended for production**

### Sampling Strategy

**Adaptive sampling:**

```yaml
# Development: Trace everything
tracing_sample_rate: 1.0

# Production (low traffic): 10% sampling
tracing_sample_rate: 0.1

# Production (high traffic): 1% sampling
tracing_sample_rate: 0.01
```

**Cost-benefit analysis:**
- 100% sampling: Full visibility, higher overhead
- 10% sampling: Good balance for most workloads
- 1% sampling: Minimal overhead, sufficient for trends

## Troubleshooting

### Traces Not Appearing

**Issue:** No traces in backend (Jaeger, Tempo, etc.)

**Solutions:**

1. **Verify tracing enabled:**
   ```yaml
   global:
     tracing_enabled: true
   ```

2. **Check endpoint connectivity:**
   ```bash
   # Test connection to Jaeger
   telnet jaeger 4317

   # Test connection to Tempo
   telnet tempo 4317
   ```

3. **Check sampling rate:**
   ```yaml
   global:
     tracing_sample_rate: 1.0  # Ensure not 0.0
   ```

4. **Use stdout exporter for debugging:**
   ```yaml
   global:
     tracing_exporter: stdout  # Verify spans are created
   ```

### Connection Refused

**Issue:** `connection refused` error in logs

**Solutions:**

1. **Verify endpoint format:**
   ```yaml
   # Correct
   tracing_endpoint: localhost:4317

   # Wrong
   tracing_endpoint: http://localhost:4317  # Don't include protocol
   ```

2. **Check backend is running:**
   ```bash
   docker ps | grep jaeger
   # OR
   docker ps | grep tempo
   ```

3. **Verify port mapping:**
   ```yaml
   # docker-compose.yml
   services:
     jaeger:
       ports:
         - "4317:4317"  # Must match tracing_endpoint port
   ```

### High Overhead

**Issue:** Performance degradation with tracing enabled

**Solutions:**

1. **Reduce sampling rate:**
   ```yaml
   global:
     tracing_sample_rate: 0.1  # Reduce from 1.0
   ```

2. **Switch from stdout to OTLP:**
   ```yaml
   global:
     tracing_exporter: otlp-grpc  # Much more efficient than stdout
   ```

3. **Disable if not needed:**
   ```yaml
   global:
     tracing_enabled: false
   ```

### Incomplete Traces

**Issue:** Missing child spans or broken traces

**Cause:** Sampling decision made at root span

**Solution:**
- OpenTelemetry samples at trace level (root span)
- If root is sampled, all children are included
- If root is not sampled, entire trace is dropped
- This is expected behavior for sampling <100%

## Best Practices

### 1. Use OTLP gRPC in Production

```yaml
# ✅ Production
global:
  tracing_exporter: otlp-grpc
  tracing_endpoint: tempo:4317

# ❌ Not for production
global:
  tracing_exporter: stdout  # High overhead
```

### 2. Adjust Sampling by Environment

```yaml
# Development
tracing_sample_rate: 1.0

# Staging
tracing_sample_rate: 0.5

# Production (low traffic)
tracing_sample_rate: 0.1

# Production (high traffic)
tracing_sample_rate: 0.01
```

### 3. Use Meaningful Service Names

```yaml
# ✅ Good (includes environment)
global:
  tracing_service_name: cbox-init-production

# ❌ Generic
global:
  tracing_service_name: cbox-init
```

### 4. Combine with Metrics

**Correlate traces with metrics:**
- Use traces for deep debugging
- Use metrics for trends and alerts
- Both provide complementary insights

### 5. Set Up Alerts

**Alert on high error rates:**
```promql
# Prometheus alert
- alert: HighTraceErrorRate
  expr: rate(trace_errors_total[5m]) > 0.1
```

## Future Enhancements

**Planned features:**
- TLS support for OTLP gRPC
- Additional exporters (Jaeger native, Zipkin)
- HTTP health check span instrumentation
- Custom span events for process state changes
- Trace context propagation to child processes

## Next Steps

- [Prometheus Metrics](metrics) - Complementary observability
- [Resource Monitoring](resource-monitoring) - Resource usage tracking
- [Management API](api) - Runtime process control

---
title: "Observability"
description: "Monitoring, metrics, tracing, and API access for production visibility and debugging"
weight: 40
---

# Observability

Cbox Init provides comprehensive observability features for monitoring, debugging, and integrating with your existing infrastructure.

## In This Section

- [Prometheus Metrics](metrics) - Export metrics for dashboards and alerting
- [Distributed Tracing](tracing) - OpenTelemetry integration for request tracing
- [Resource Monitoring](resource-monitoring) - CPU, memory, and process resource tracking
- [Management API](api) - REST API for programmatic access and automation

## Quick Overview

### Metrics & Monitoring

Cbox Init exposes Prometheus-compatible metrics for:
- Process state and health
- Resource usage (CPU, memory, file descriptors)
- Restart counts and failure rates
- Scheduled task execution statistics

### Tracing

OpenTelemetry-based distributed tracing provides:
- Process lifecycle spans
- Integration with Jaeger, Grafana Tempo, and other backends
- Configurable sampling rates for production

### API Access

The Management API enables:
- Runtime process control (start/stop/restart/scale)
- Configuration management
- Health status queries
- Integration with external orchestration tools

## Configuration Overview

```yaml
global:
  # Prometheus metrics
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: /metrics

  # Resource monitoring
  resource_metrics_enabled: true
  resource_metrics_interval: 5

  # Distributed tracing
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: localhost:4317

  # Management API
  api_enabled: true
  api_port: 9180
  api_socket: /var/run/cbox-init.sock
```

See individual pages for detailed configuration options.

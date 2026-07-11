---
title: "Laravel with Monitoring"
description: "Production Laravel setup with Prometheus metrics, Management API, and complete observability"
weight: 33
---

# Laravel with Monitoring

Production Laravel deployment with comprehensive monitoring, metrics, and runtime management via API.

## Use Cases

- ✅ Production observability and alerting
- ✅ Runtime process control and inspection
- ✅ Performance monitoring and optimization
- ✅ Capacity planning and resource tracking
- ✅ SRE and DevOps workflows

## Features Enabled

**Observability:**
- ✅ Prometheus metrics on port 9090
- ✅ Management API on port 9180
- ✅ Health check monitoring
- ✅ Process lifecycle tracking
- ✅ Hook execution metrics

**Monitoring:**
- ✅ Process uptime and restarts
- ✅ Health status per process
- ✅ Resource usage tracking
- ✅ Queue depth and processing rates

## Complete Configuration

```yaml
version: "1.0"

global:
  shutdown_timeout: 30
  log_level: info
  log_format: json

  # Prometheus metrics
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: /metrics

  # Management API
  api_enabled: true
  api_port: 9180
  api_auth: "your-secure-token-here"

hooks:
  pre-start:
    - name: config-cache
      command: ["php", "artisan", "config:cache"]
      timeout: 60

    - name: migrate
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
    health_check:
      type: tcp
      address: 127.0.0.1:9000
      period: 10

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    health_check:
      type: http
      url: http://127.0.0.1:80/health
      expected_status: 200

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    health_check:
      type: exec
      command: ["php", "artisan", "horizon:status"]
      period: 30
    shutdown:
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60
      timeout: 120

  queue-default:
    enabled: true
    command: ["php", "artisan", "queue:work"]
    scale: 3
```

## Prometheus Metrics

### Available Metrics

```bash
# Check all metrics
curl http://localhost:9090/metrics

# Key metrics:
cbox_init_manager_uptime_seconds               # Manager uptime
cbox_init_process_up{process="nginx"}          # Process status (1=up, 0=down)
cbox_init_process_restarts_total{process="*"}  # Restart count
cbox_init_process_health_status{process="*"}   # Health status
cbox_init_process_start_time{process="*"}      # Process start timestamp
cbox_init_hook_execution_seconds{hook="*"}     # Hook execution time
```

### Grafana Dashboard

**Add Prometheus data source:**
```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    url: http://prometheus:9090
    isDefault: true
```

**Example Queries:**
```promql
# Process uptime
cbox_init_manager_uptime_seconds

# Total restarts (all processes)
sum(cbox_init_process_restarts_total)

# Unhealthy processes
count(cbox_init_process_health_status == 0)

# Processes by state
count by (state) (cbox_init_process_up)

# Hook execution duration
rate(cbox_init_hook_execution_seconds_sum[5m]) /
rate(cbox_init_hook_execution_seconds_count[5m])
```

### Prometheus Alerts

**alerts.yml:**
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
          summary: "Process {{ $labels.process }} is down"
          description: "{{ $labels.process }} has been down for 1 minute"

      # Excessive restarts
      - alert: FrequentRestarts
        expr: rate(cbox_init_process_restarts_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Process {{ $labels.process }} restarting frequently"

      # Unhealthy process
      - alert: ProcessUnhealthy
        expr: cbox_init_process_health_status == 0
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Process {{ $labels.process }} is unhealthy"

      # Hook failures
      - alert: HookFailed
        expr: cbox_init_hook_failures_total > 0
        labels:
          severity: warning
        annotations:
          summary: "Hook {{ $labels.hook }} failed"
```

## Management API

### Authentication

```bash
# Set API token
export API_TOKEN="your-secure-token-here"

# All requests require Bearer token
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:9180/api/v1/health
```

### API Endpoints

#### GET /api/v1/health

**Check API health:**
```bash
curl http://localhost:9180/api/v1/health

# Response:
{
  "status": "healthy",
  "timestamp": "2024-11-21T10:00:00Z"
}
```

#### GET /api/v1/processes

**List all processes:**
```bash
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:9180/api/v1/processes | jq

# Response:
[
  {
    "name": "php-fpm",
    "state": "running",
    "health_status": "healthy",
    "pid": 123,
    "uptime": 3600,
    "restart_count": 0,
    "last_exit_code": 0
  },
  {
    "name": "nginx",
    "state": "running",
    "health_status": "healthy",
    "pid": 124,
    "uptime": 3550,
    "restart_count": 0
  }
]
```

#### POST /api/v1/processes/{name}/restart

**Restart a process:**
```bash
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:9180/api/v1/processes/nginx/restart

# Response:
{
  "status": "restarting",
  "process": "nginx",
  "message": "Process restart initiated"
}
```

#### POST /api/v1/processes/{name}/stop

**Stop a process:**
```bash
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:9180/api/v1/processes/queue-default/stop

# Response:
{
  "status": "stopping",
  "process": "queue-default"
}
```

#### POST /api/v1/processes/{name}/start

**Start a stopped process:**
```bash
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:9180/api/v1/processes/queue-default/start

# Response:
{
  "status": "starting",
  "process": "queue-default"
}
```

#### POST /api/v1/processes/{name}/scale

**Scale process instances:**
```bash
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"desired": 5}' \
  http://localhost:9180/api/v1/processes/queue-default/scale

# Response:
{
  "status": "scaling",
  "process": "queue-default",
  "current": 3,
  "desired": 5
}
```

## Monitoring Workflows

### Production Deployment Checklist

```bash
#!/bin/bash
# deploy-with-monitoring.sh
set -e

API_URL="http://localhost:9180/api/v1"
TOKEN="your-api-token"

echo "1. Check current health..."
curl -H "Authorization: Bearer $TOKEN" "$API_URL/processes" | jq '.[] | {name, health_status}'

echo "2. Deploy new version..."
docker-compose up -d

echo "3. Wait for health checks..."
sleep 30

echo "4. Verify all processes healthy..."
curl -H "Authorization: Bearer $TOKEN" "$API_URL/processes" | jq '.[] | select(.health_status!="healthy")'

echo "5. Check metrics..."
curl http://localhost:9090/metrics | grep "cbox_init_process_up"

echo "Deployment complete!"
```

### Scale Queue Workers

```bash
#!/bin/bash
# scale-queues.sh
API_URL="http://localhost:9180/api/v1"
TOKEN="your-api-token"

# Get current queue depth
QUEUE_DEPTH=$(php artisan queue:size default)

# Scale based on queue depth
if [ "$QUEUE_DEPTH" -gt 100 ]; then
    echo "Queue depth high ($QUEUE_DEPTH), scaling to 10 workers..."
    curl -X POST \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"desired": 10}' \
      "$API_URL/processes/queue-default/scale"
elif [ "$QUEUE_DEPTH" -lt 10 ]; then
    echo "Queue depth low ($QUEUE_DEPTH), scaling to 2 workers..."
    curl -X POST \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"desired": 2}' \
      "$API_URL/processes/queue-default/scale"
fi
```

### Automated Restart on Failure

```bash
#!/bin/bash
# health-monitor.sh
API_URL="http://localhost:9180/api/v1"
TOKEN="your-api-token"

while true; do
    # Check process health
    UNHEALTHY=$(curl -s -H "Authorization: Bearer $TOKEN" "$API_URL/processes" | \
                jq -r '.[] | select(.health_status=="unhealthy") | .name')

    if [ -n "$UNHEALTHY" ]; then
        echo "Unhealthy process detected: $UNHEALTHY"
        echo "Attempting restart..."

        curl -X POST \
          -H "Authorization: Bearer $TOKEN" \
          "$API_URL/processes/$UNHEALTHY/restart"
    fi

    sleep 60
done
```

## Docker Compose with Monitoring Stack

```yaml
version: '3.8'

services:
  # Laravel application with Cbox Init
  app:
    build: .
    ports:
      - "80:80"
      - "9090:9090"  # Prometheus metrics
      - "9180:9180"  # Management API
    environment:
      PHP_FPM_AUTOTUNE_PROFILE: "medium"
      CBOX_INIT_GLOBAL_METRICS_ENABLED: "true"
      CBOX_INIT_GLOBAL_API_ENABLED: "true"
      CBOX_INIT_GLOBAL_API_AUTH: ${API_TOKEN}
    depends_on:
      - database
      - redis
    networks:
      - app-network
      - monitoring

  # Prometheus metrics collection
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./alerts.yml:/etc/prometheus/alerts.yml
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
    networks:
      - monitoring

  # Grafana dashboards
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_PASSWORD}
      GF_USERS_ALLOW_SIGN_UP: "false"
    volumes:
      - grafana-data:/var/lib/grafana
      - ./grafana-dashboards:/etc/grafana/provisioning/dashboards
    networks:
      - monitoring

  # Alertmanager for notifications
  alertmanager:
    image: prom/alertmanager:latest
    ports:
      - "9093:9093"
    volumes:
      - ./alertmanager.yml:/etc/alertmanager/alertmanager.yml
      - alertmanager-data:/alertmanager
    networks:
      - monitoring

  database:
    image: mysql:8.0
    environment:
      MYSQL_DATABASE: laravel
      MYSQL_PASSWORD: ${DB_PASSWORD}
    volumes:
      - db-data:/var/lib/mysql
    networks:
      - app-network

  redis:
    image: redis:alpine
    volumes:
      - redis-data:/data
    networks:
      - app-network

volumes:
  db-data:
  redis-data:
  prometheus-data:
  grafana-data:
  alertmanager-data:

networks:
  app-network:
  monitoring:
```

## Prometheus Configuration

**prometheus.yml:**
```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

# Load alert rules
rule_files:
  - /etc/prometheus/alerts.yml

# Scrape configs
scrape_configs:
  # Cbox Init metrics
  - job_name: 'cbox-init'
    static_configs:
      - targets: ['app:9090']
        labels:
          env: 'production'
          app: 'laravel'

# Alert routing
alerting:
  alertmanagers:
    - static_configs:
        - targets: ['alertmanager:9093']
```

## Alertmanager Configuration

**alertmanager.yml:**
```yaml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'cluster', 'service']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'default'

  routes:
    # Critical alerts to PagerDuty
    - match:
        severity: critical
      receiver: 'pagerduty'

    # Warnings to Slack
    - match:
        severity: warning
      receiver: 'slack'

receivers:
  - name: 'default'
    email_configs:
      - to: 'team@example.com'

  - name: 'pagerduty'
    pagerduty_configs:
      - service_key: 'your-pagerduty-key'

  - name: 'slack'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/XXX'
        channel: '#alerts'
```

## Grafana Dashboard

### Import Dashboard JSON

```json
{
  "dashboard": {
    "title": "Cbox Init - Laravel Monitoring",
    "panels": [
      {
        "title": "Process Uptime",
        "targets": [{
          "expr": "cbox_init_manager_uptime_seconds"
        }]
      },
      {
        "title": "Process Status",
        "targets": [{
          "expr": "cbox_init_process_up"
        }]
      },
      {
        "title": "Restart Rate",
        "targets": [{
          "expr": "rate(cbox_init_process_restarts_total[5m])"
        }]
      },
      {
        "title": "Health Check Status",
        "targets": [{
          "expr": "cbox_init_process_health_status"
        }]
      },
      {
        "title": "Hook Execution Time",
        "targets": [{
          "expr": "rate(cbox_init_hook_execution_seconds_sum[5m]) / rate(cbox_init_hook_execution_seconds_count[5m])"
        }]
      }
    ]
  }
}
```

## Management API Usage

### Process Control

```bash
# API base URL
API_URL="http://localhost:9180/api/v1"
TOKEN="your-secure-token-here"

# Restart Nginx (zero-downtime reload)
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "$API_URL/processes/nginx/restart"

# Stop queue workers for maintenance
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "$API_URL/processes/queue-default/stop"

# Start queue workers after maintenance
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "$API_URL/processes/queue-default/start"

# Scale queue workers dynamically
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"desired": 5}' \
  "$API_URL/processes/queue-default/scale"
```

### Monitoring Scripts

**check-health.sh:**
```bash
#!/bin/bash
API_URL="http://localhost:9180/api/v1"
TOKEN="your-api-token"

# Get all process health
RESPONSE=$(curl -s -H "Authorization: Bearer $TOKEN" "$API_URL/processes")

# Check for unhealthy processes
UNHEALTHY=$(echo "$RESPONSE" | jq -r '.[] | select(.health_status=="unhealthy") | .name')

if [ -n "$UNHEALTHY" ]; then
    echo "CRITICAL: Unhealthy processes: $UNHEALTHY"
    exit 1
else
    echo "OK: All processes healthy"
    exit 0
fi
```

**auto-scale-queues.sh:**
```bash
#!/bin/bash
API_URL="http://localhost:9180/api/v1"
TOKEN="your-api-token"

# Get current queue depth (Laravel Horizon)
QUEUE_DEPTH=$(php artisan queue:size)

# Calculate desired workers
if [ "$QUEUE_DEPTH" -gt 1000 ]; then
    DESIRED=20
elif [ "$QUEUE_DEPTH" -gt 500 ]; then
    DESIRED=10
elif [ "$QUEUE_DEPTH" -gt 100 ]; then
    DESIRED=5
else
    DESIRED=2
fi

echo "Queue depth: $QUEUE_DEPTH, scaling to $DESIRED workers"

curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"desired\": $DESIRED}" \
  "$API_URL/processes/queue-default/scale"
```

## Complete Monitoring Stack

### docker-compose.monitoring.yml

```yaml
version: '3.8'

services:
  # Cbox Init with full observability
  app:
    image: myapp:latest
    environment:
      CBOX_INIT_GLOBAL_METRICS_ENABLED: "true"
      CBOX_INIT_GLOBAL_API_ENABLED: "true"
      CBOX_INIT_GLOBAL_API_AUTH: ${API_TOKEN}
    networks:
      - app-network
      - monitoring

  # Prometheus
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./monitoring/prometheus.yml:/etc/prometheus/prometheus.yml
      - ./monitoring/alerts.yml:/etc/prometheus/alerts.yml
      - prometheus-data:/prometheus
    networks:
      - monitoring

  # Grafana
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_PASSWORD}
    volumes:
      - ./monitoring/dashboards:/etc/grafana/provisioning/dashboards
      - ./monitoring/datasources:/etc/grafana/provisioning/datasources
      - grafana-data:/var/lib/grafana
    networks:
      - monitoring

  # Alertmanager
  alertmanager:
    image: prom/alertmanager:latest
    ports:
      - "9093:9093"
    volumes:
      - ./monitoring/alertmanager.yml:/etc/alertmanager/alertmanager.yml
    networks:
      - monitoring

  # Loki log aggregation
  loki:
    image: grafana/loki:latest
    ports:
      - "3100:3100"
    volumes:
      - ./monitoring/loki-config.yml:/etc/loki/local-config.yaml
      - loki-data:/loki
    networks:
      - monitoring

  # Promtail log shipper
  promtail:
    image: grafana/promtail:latest
    volumes:
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - ./monitoring/promtail-config.yml:/etc/promtail/config.yml
    networks:
      - monitoring

volumes:
  prometheus-data:
  grafana-data:
  loki-data:

networks:
  app-network:
  monitoring:
```

## Alerting Examples

### Slack Notifications

```yaml
# alertmanager.yml
receivers:
  - name: 'slack'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/XXX/YYY/ZZZ'
        channel: '#production-alerts'
        title: 'Cbox Init Alert'
        text: '{{ range .Alerts }}{{ .Annotations.summary }}\n{{ end }}'
```

### PagerDuty Integration

```yaml
receivers:
  - name: 'pagerduty'
    pagerduty_configs:
      - service_key: 'your-pagerduty-key'
        description: '{{ .CommonAnnotations.summary }}'
```

### Email Alerts

```yaml
receivers:
  - name: 'email'
    email_configs:
      - to: 'sre-team@example.com'
        from: 'alerts@example.com'
        smarthost: 'smtp.gmail.com:587'
        auth_username: 'alerts@example.com'
        auth_password: 'your-app-password'
```

## Monitoring Best Practices

### ✅ Do

**Set alert thresholds appropriately:**
```yaml
# Allow brief failures before alerting
- alert: ProcessDown
  expr: cbox_init_process_up == 0
  for: 2m  # Not instant, allow for brief restarts
```

**Monitor trends, not just state:**
```yaml
# Alert on increasing restart rate
- alert: RestartRateIncreasing
  expr: rate(cbox_init_process_restarts_total[15m]) >
        rate(cbox_init_process_restarts_total[1h])
```

**Use severity labels:**
```yaml
labels:
  severity: critical  # Page on-call
  severity: warning   # Notify team
  severity: info      # Log only
```

**Secure API access:**
```yaml
global:
  api_auth: "use-strong-random-token-here"  # Generate with: openssl rand -base64 32
```

### ❌ Don't

**Don't alert on expected behavior:**
```yaml
# ❌ Bad - scheduled tasks restart by design
- alert: SchedulerRestarted
  expr: cbox_init_process_restarts_total{process="scheduler"} > 0

# ✅ Good - only alert on unexpected restarts
- alert: UnexpectedRestart
  expr: cbox_init_process_restarts_total{process!~"scheduler|.*-task"} > threshold
```

**Don't expose API publicly:**
```yaml
# ❌ Bad
ports:
  - "0.0.0.0:9180:9180"

# ✅ Good - only internal
expose:
  - "9180"
```

## Security Hardening

### API Token Management

```bash
# Generate strong token
API_TOKEN=$(openssl rand -base64 32)

# Store in secret management
echo $API_TOKEN > /secrets/cbox-api-token

# Use in configuration
export CBOX_INIT_GLOBAL_API_AUTH=$(cat /secrets/cbox-api-token)
```

### Network Isolation

```yaml
networks:
  app-network:
    internal: true  # No external access

  monitoring:
    internal: false  # External access for dashboards
```

### Rate Limiting (Nginx Proxy)

```nginx
# nginx-api-proxy.conf
http {
    limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;

    server {
        listen 9180;

        location /api/ {
            limit_req zone=api burst=20;
            proxy_pass http://app:9180;
        }
    }
}
```

## Troubleshooting

### Metrics Not Scraping

**Check Prometheus targets:**
```
http://localhost:9091/targets
```

**Verify app metrics accessible:**
```bash
curl http://localhost:9090/metrics | head
```

### API Authentication Failing

**Test without authentication:**
```bash
# Check if API is reachable
curl http://localhost:9180/api/v1/health

# If this works, check token
echo $API_TOKEN
```

**Verify token in container:**
```bash
docker-compose exec app env | grep CBOX_INIT_GLOBAL_API_AUTH
```

### Grafana Not Showing Data

**Check Prometheus data source:**
- Settings → Data Sources → Prometheus
- URL should be `http://prometheus:9090`
- Save & Test

**Test query in Explore:**
```promql
cbox_init_process_up
```

## See Also

- [Prometheus Metrics](../observability/metrics) - Complete metrics reference
- [Management API](../observability/api) - API documentation
- [Docker Compose](docker-compose) - Docker Compose patterns
- [Health Checks](../configuration/health-checks) - Health monitoring

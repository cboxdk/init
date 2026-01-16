---
title: "Process Scaling"
description: "Multi-instance process management with dynamic scaling and load distribution"
weight: 23
---

# Process Scaling

Run multiple instances of the same process for load distribution, high availability, and parallel processing.

## Overview

Process scaling enables:
- ✅ **Horizontal scaling:** Run N copies of the same process
- ✅ **Load distribution:** Distribute work across instances
- ✅ **High availability:** Continue operating if instances fail
- ✅ **Resource optimization:** Match capacity to demand
- ✅ **Zero-downtime updates:** Rolling restarts

## Basic Configuration

```yaml
processes:
  queue-default:
    enabled: true
    command: ["php", "artisan", "queue:work"]
    scale: 3  # Run 3 instances
```

**Result:** Creates 3 processes:
- `queue-default-1`
- `queue-default-2`
- `queue-default-3`

## Use Cases

### Queue Workers

**Problem:** Single worker can't keep up with job queue

**Solution:** Scale horizontally

```yaml
processes:
  queue-default:
    command: ["php", "artisan", "queue:work", "--queue=default"]
    scale: 5  # 5 parallel workers
    restart: always

  queue-high:
    command: ["php", "artisan", "queue:work", "--queue=high"]
    scale: 2  # 2 workers for high-priority queue
```

**Benefits:**
- Process 5x more jobs simultaneously
- Reduce queue latency
- Better resource utilization

### Background Workers

```yaml
processes:
  data-processor:
    command: ["./process-worker"]
    scale: 10  # 10 parallel processors
    env:
      WORKER_TYPE: data_processor
```

### High Availability

```yaml
processes:
  api-server:
    command: ["./api-server"]
    scale: 3  # 3 instances for redundancy
    restart: always
```

**Benefits:**
- If 1 instance crashes, 2 others continue
- Rolling updates possible
- No single point of failure

## Instance Naming

### Automatic Naming

```yaml
processes:
  worker:
    command: ["./worker"]
    scale: 3
```

**Creates:**
- `worker-1`
- `worker-2`
- `worker-3`

### Environment Variables per Instance

Each instance receives unique environment variables:

```bash
# Instance 1
CBOX_INIT_INSTANCE_ID=worker-1
CBOX_INIT_INSTANCE_NUMBER=1

# Instance 2
CBOX_INIT_INSTANCE_ID=worker-2
CBOX_INIT_INSTANCE_NUMBER=2

# Instance 3
CBOX_INIT_INSTANCE_ID=worker-3
CBOX_INIT_INSTANCE_NUMBER=3
```

**Use in application:**
```php
$instanceId = getenv('CBOX_INIT_INSTANCE_ID');
$instanceNum = getenv('CBOX_INIT_INSTANCE_NUMBER');

Log::info("Worker starting", [
    'instance_id' => $instanceId,
    'instance_number' => $instanceNum
]);
```

## Dynamic Scaling

### Via Environment Variables

```bash
# Start with 3 workers
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=3 ./cbox-init

# Scale to 10 workers (restart required)
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=10 ./cbox-init
```

### Via Management API

```bash
# Runtime scaling (no restart)
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"desired": 10}' \
  http://localhost:9180/api/v1/processes/queue-default/scale

# Response:
{
  "status": "scaling",
  "process": "queue-default",
  "current": 3,
  "desired": 10,
  "message": "Scaling from 3 to 10 instances"
}
```

**Behavior:**
- **Scale up:** Start new instances (queue-default-4 through queue-default-10)
- **Scale down:** Stop excess instances gracefully
- **Zero downtime:** Existing instances continue running

### Auto-Scaling Script

```bash
#!/bin/bash
# auto-scale-queues.sh
API_URL="http://localhost:9180/api/v1"
TOKEN="your-api-token"

while true; do
    # Get queue depth
    QUEUE_DEPTH=$(php artisan queue:size default)

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

    # Scale if needed
    CURRENT=$(curl -s -H "Authorization: Bearer $TOKEN" "$API_URL/processes/queue-default" | jq '.scale')

    if [ "$CURRENT" != "$DESIRED" ]; then
        echo "Scaling from $CURRENT to $DESIRED workers (queue depth: $QUEUE_DEPTH)"
        curl -X POST \
          -H "Authorization: Bearer $TOKEN" \
          -H "Content-Type: application/json" \
          -d "{\"desired\": $DESIRED}" \
          "$API_URL/processes/queue-default/scale"
    fi

    sleep 60
done
```

## Scaling Strategies

### CPU-Based Scaling

```yaml
# Scale based on available CPUs
# 1 worker per CPU core
queue-workers:
  scale: ${CPU_COUNT}  # e.g., 4 CPUs → 4 workers
```

**Docker Compose:**
```yaml
services:
  app:
    environment:
      CPU_COUNT: "4"
      CBOX_INIT_PROCESS_QUEUE_WORKERS_SCALE: "4"
    deploy:
      resources:
        limits:
          cpus: '4'
```

### Memory-Based Scaling

```yaml
# Calculate workers from memory
# Example: 4GB RAM, 400MB per worker = 10 workers
queue-workers:
  scale: ${WORKER_COUNT}  # Calculated externally
```

**Calculate:**
```bash
MEMORY_GB=4
MEMORY_PER_WORKER_MB=400
WORKER_COUNT=$((MEMORY_GB * 1024 / MEMORY_PER_WORKER_MB))

export CBOX_INIT_PROCESS_QUEUE_WORKERS_SCALE=$WORKER_COUNT
```

### Queue-Depth-Based Scaling

```bash
# Scale based on queue depth
QUEUE_DEPTH=$(redis-cli llen queues:default)
WORKERS=$((QUEUE_DEPTH / 100 + 1))  # 1 worker per 100 jobs, minimum 1

export CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=$WORKERS
```

### Time-Based Scaling

```bash
# More workers during business hours
HOUR=$(date +%H)

if [ "$HOUR" -ge 9 ] && [ "$HOUR" -le 17 ]; then
    WORKERS=10  # Business hours
else
    WORKERS=2   # Off hours
fi

curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"desired\": $WORKERS}" \
  "$API_URL/processes/queue-default/scale"
```

## Load Distribution

### Queue Workers

PHP queue workers automatically distribute jobs (Redis or database-backed):

```yaml
processes:
  queue-default:
    command: ["php", "artisan", "queue:work", "--queue=default"]
    scale: 5
```

**How it works:**
1. All 5 workers connect to same Redis queue
2. Redis ensures each job processed by one worker (atomic pop)
3. Natural load distribution across workers

### HTTP Workers (Round-Robin)

```yaml
processes:
  api-worker:
    command: ["./api-server", "--port=8080"]
    scale: 3
```

**Load balancer (Nginx):**
```nginx
upstream api_backend {
    server 127.0.0.1:8081;  # api-worker-1
    server 127.0.0.1:8082;  # api-worker-2
    server 127.0.0.1:8083;  # api-worker-3
}

server {
    location /api/ {
        proxy_pass http://api_backend;
    }
}
```

## Monitoring Scaled Processes

### Prometheus Metrics

```bash
# Count running instances
count(cbox_init_process_up{process=~"queue-default-.*"})

# Per-instance uptime
cbox_init_process_up{process="queue-default-1"}
cbox_init_process_up{process="queue-default-2"}
cbox_init_process_up{process="queue-default-3"}

# Total restarts across all instances
sum(cbox_init_process_restarts_total{process=~"queue-default-.*"})
```

### Management API

```bash
# List all instances
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9180/api/v1/processes | \
  jq '.[] | select(.name | startswith("queue-default-"))'

# Response:
[
  {
    "name": "queue-default-1",
    "state": "running",
    "pid": 123,
    "uptime": 3600
  },
  {
    "name": "queue-default-2",
    "state": "running",
    "pid": 124,
    "uptime": 3600
  },
  {
    "name": "queue-default-3",
    "state": "running",
    "pid": 125,
    "uptime": 3600
  }
]
```

## Troubleshooting

### Scaling Up Fails

**Check resource limits:**
```bash
# Verify container has enough memory
docker stats app

# Check if hitting CPU limits
docker exec app ps aux
```

**Solution:**
```yaml
services:
  app:
    deploy:
      resources:
        limits:
          memory: 8G  # Increase memory
          cpus: '8'   # Increase CPU
```

### Scaling Down Doesn't Work

**Check grace period:**
```yaml
processes:
  worker:
    shutdown:
      timeout: 60  # Allow workers to finish gracefully
```

**Force scale down:**
```bash
# Stop specific instance
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:9180/api/v1/processes/queue-default-5/stop
```

### Uneven Load Distribution

**Problem:** Some workers idle, others busy

**Solution (Queue Workers):**
```yaml
# Reduce max-time so workers restart and rebalance
queue-default:
  command: ["php", "artisan", "queue:work", "--max-time=1800"]  # 30 min
  scale: 5
```

**Solution (HTTP Workers):**
```nginx
# Use least_conn load balancing
upstream api_backend {
    least_conn;  # Send to least busy worker
    server 127.0.0.1:8081;
    server 127.0.0.1:8082;
    server 127.0.0.1:8083;
}
```

## Advanced Patterns

### Per-Instance Configuration

```yaml
processes:
  worker:
    command: ["./worker", "--id=${CBOX_INIT_INSTANCE_NUMBER}"]
    scale: 3
```

**worker script:**
```bash
#!/bin/bash
INSTANCE_ID=$1

echo "Worker instance $INSTANCE_ID starting"

# Instance-specific configuration
case $INSTANCE_ID in
    1) SHARD="shard-a" ;;
    2) SHARD="shard-b" ;;
    3) SHARD="shard-c" ;;
esac

./process-shard $SHARD
```

### Gradual Scale-Up

```bash
#!/bin/bash
# gradual-scale.sh
API_URL="http://localhost:9180/api/v1"
TOKEN="your-api-token"

CURRENT=2
TARGET=20
STEP=2
DELAY=30

while [ $CURRENT -lt $TARGET ]; do
    CURRENT=$((CURRENT + STEP))
    echo "Scaling to $CURRENT workers..."

    curl -X POST \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"desired\": $CURRENT}" \
      "$API_URL/processes/queue-default/scale"

    echo "Waiting ${DELAY}s before next increment..."
    sleep $DELAY
done

echo "Reached target: $TARGET workers"
```

### Canary Scaling

```yaml
processes:
  # Stable version
  app-stable:
    command: ["./app", "--version=stable"]
    scale: 9  # 90% of traffic

  # Canary version
  app-canary:
    command: ["./app", "--version=canary"]
    scale: 1  # 10% of traffic
```

**Monitor canary:**
```bash
# Compare error rates
curl http://localhost:9090/metrics | grep 'error_rate{version="canary"}'
curl http://localhost:9090/metrics | grep 'error_rate{version="stable"}'

# If canary is good, promote
curl -X POST -d '{"desired": 10}' "$API_URL/processes/app-canary/scale"
curl -X POST -d '{"desired": 0}' "$API_URL/processes/app-stable/scale"
```

## See Also

- [Process Configuration](../configuration/processes) - Scale configuration
- [Management API](../observability/api) - Runtime scaling API
- [Examples](../examples/) - Queue worker scaling
- [Prometheus Metrics](../observability/metrics) - Scaling metrics

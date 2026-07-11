---
title: "Quick Start"
description: "Run your first multi-process container with Cbox Init in 5 minutes"
weight: 4
---

# Quick Start

Get Cbox Init running with a complete PHP-FPM and Nginx stack in just 5 minutes.

## Step 1: Create Configuration

Create `cbox-init.yaml`:

```yaml
version: "1.0"

global:
  shutdown_timeout: 30
  log_level: info
  log_format: json

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always

    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      initial_delay: 5
      period: 10
      failure_threshold: 3
      success_threshold: 2

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    restart: always
    depends_on: [php-fpm]

    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      expected_status: 200
      initial_delay: 3
      period: 10
      failure_threshold: 3
```

## Step 2: Create Dockerfile

```dockerfile
FROM php:8.3-fpm-alpine

# Install Nginx
RUN apk add --no-cache nginx

# Copy Cbox Init binary
COPY --from=cboxdk/init:latest \
    /usr/local/bin/cbox-init \
    /usr/local/bin/cbox-init

# Copy configuration
COPY cbox-init.yaml /etc/cbox-init/cbox-init.yaml

# Copy Nginx config
COPY nginx.conf /etc/nginx/nginx.conf

# Copy application
COPY . /var/www/html
WORKDIR /var/www/html

# Use Cbox Init as PID 1
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## Step 3: Build and Run

```bash
# Build image
docker build -t my-php-app .

# Run container
docker run -d \
    --name php-app \
    -p 8080:80 \
    my-php-app

# View logs
docker logs -f php-app
```

## Step 4: Verify Processes

Check that both processes are running:

```bash
# View process status
docker exec php-app ps aux

# Expected output:
# PID   USER     COMMAND
#   1   root     /usr/local/bin/cbox-init
#  10   www-data php-fpm: master process
#  11   www-data php-fpm: pool www
#  12   www-data php-fpm: pool www
#  20   nginx    nginx: master process
#  21   nginx    nginx: worker process
```

## Step 5: Test Health Checks

Health checks run automatically:

```bash
# Check logs for health check results
docker logs php-app 2>&1 | grep "health check"

# Example output:
# {"level":"INFO","msg":"Health check passed","process":"php-fpm","type":"tcp"}
# {"level":"INFO","msg":"Health check passed","process":"nginx","type":"http"}
```

## Step 6: Test Graceful Shutdown

```bash
# Send SIGTERM to container
docker stop php-app

# Cbox Init will:
# 1. Stop accepting new requests
# 2. Signal processes in reverse order (nginx, then php-fpm)
# 3. Wait for graceful shutdown (30s timeout)
# 4. Exit cleanly
```

## What Just Happened?

Cbox Init orchestrated:

1. **Startup Order**
   - Started PHP-FPM first (priority 10)
   - Waited for PHP-FPM health check
   - Started Nginx second (priority 20)
   - Honored `depends_on` relationship

2. **Health Monitoring**
   - TCP check on PHP-FPM port 9000
   - HTTP check on Nginx endpoint
   - Automatic restart if checks fail

3. **Graceful Shutdown**
   - Stopped Nginx first (reverse order)
   - Then stopped PHP-FPM
   - Clean process termination

## Framework Configuration Patterns

### Laravel Application

```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    shutdown:
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60

  queue-worker:
    enabled: true
    command: ["php", "artisan", "queue:work", "--tries=3"]
    scale: 3
```

### Symfony Application

```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  messenger:
    enabled: true
    command: ["php", "bin/console", "messenger:consume", "async", "--time-limit=3600"]
    scale: 2
    restart: always
```

### WordPress Application

```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  wp-cron:
    enabled: true
    command: ["php", "/var/www/html/wp-cron.php"]
    schedule: "*/5 * * * *"  # Run every 5 minutes
```

### With Observability

```yaml
global:
  # Enable Prometheus metrics
  metrics_enabled: true
  metrics_port: 9090

  # Enable management API
  api_enabled: true
  api_port: 9180
  api_auth: "your-secret-token"

processes:
  # ... your processes
```

## Troubleshooting

### Processes Not Starting

Check logs for startup errors:

```bash
docker logs php-app 2>&1 | grep -i error
```

Common issues:
- Missing binary in PATH
- Wrong command syntax
- Permission issues

### Health Checks Failing

View health check details:

```bash
docker logs php-app 2>&1 | grep "health check"
```

Solutions:
- Increase `initial_delay` for slow startup
- Verify TCP port or HTTP endpoint
- Check `failure_threshold` isn't too aggressive

### Container Exits Immediately

Check exit code and logs:

```bash
docker ps -a | grep php-app
docker logs php-app
```

Usually caused by:
- Configuration syntax errors
- Required process failing to start
- Missing dependencies

## Next Steps

Now that you have a working setup:

- [Docker Integration](docker-integration) - Advanced Docker patterns
- [Configuration](../configuration/overview) - Deep dive into config options
- [Health Checks](../features/health-checks) - Master health monitoring
- [Examples](../examples) - Real-world configurations

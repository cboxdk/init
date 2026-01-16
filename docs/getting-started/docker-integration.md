---
title: "Docker Integration"
description: "Use Cbox Init as PID 1 in Docker containers for proper signal handling"
weight: 5
---

# Docker Integration

Cbox Init is designed to run as PID 1 in Docker containers, providing proper init system capabilities.

## Why Cbox Init as PID 1?

**PID 1 Responsibilities**
- Signal forwarding to child processes
- Zombie process reaping
- Clean shutdown coordination
- Process tree management

**What Cbox Init Provides**
- Proper SIGTERM/SIGINT/SIGQUIT handling
- Automatic zombie reaping
- Graceful shutdown with timeouts
- Multi-process lifecycle management

## Basic Docker Integration

### Single-Stage Dockerfile

```dockerfile
FROM php:8.3-fpm-alpine

# Install system dependencies
RUN apk add --no-cache nginx

# Install Cbox Init
COPY --from=cboxdk/init:latest \
    /usr/local/bin/cbox-init \
    /usr/local/bin/cbox-init

# Copy configuration
COPY cbox-init.yaml /etc/cbox-init/cbox-init.yaml

# Copy application
COPY . /var/www/html
WORKDIR /var/www/html

# Cbox Init as PID 1
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

### Multi-Stage Build

```dockerfile
# Build stage
FROM php:8.3-fpm-alpine AS builder

WORKDIR /app
COPY . .

# Install dependencies
RUN composer install --no-dev --optimize-autoloader

# Runtime stage
FROM php:8.3-fpm-alpine

# Install runtime dependencies
RUN apk add --no-cache nginx

# Copy Cbox Init
COPY --from=cboxdk/init:latest \
    /usr/local/bin/cbox-init \
    /usr/local/bin/cbox-init

# Copy application from builder
COPY --from=builder /app /var/www/html
WORKDIR /var/www/html

# Copy configuration
COPY docker/cbox-init.yaml /etc/cbox-init/cbox-init.yaml
COPY docker/nginx.conf /etc/nginx/nginx.conf

# Cbox Init as entrypoint
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## PHP Application Dockerfile

```dockerfile
FROM php:8.3-fpm-alpine

# Install system packages
RUN apk add --no-cache \
    nginx \
    supervisor \
    mysql-client \
    postgresql-client

# Install PHP extensions
RUN docker-php-ext-install \
    pdo_mysql \
    pdo_pgsql \
    pcntl \
    bcmath

# Install Composer
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer

# Copy Cbox Init
COPY --from=cboxdk/init:latest \
    /usr/local/bin/cbox-init \
    /usr/local/bin/cbox-init

# Application setup
COPY . /var/www/html
WORKDIR /var/www/html

# Install dependencies
RUN composer install --no-dev --optimize-autoloader

# Laravel optimization
RUN php artisan config:cache && \
    php artisan route:cache && \
    php artisan view:cache

# Copy configurations
COPY docker/cbox-init.yaml /etc/cbox-init/cbox-init.yaml
COPY docker/nginx.conf /etc/nginx/nginx.conf

# Set permissions
RUN chown -R www-data:www-data /var/www/html/storage /var/www/html/bootstrap/cache

ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## Configuration File Location

Cbox Init looks for configuration in this order:

1. `CBOX_INIT_CONFIG` environment variable
2. `/etc/cbox-init/cbox-init.yaml`
3. `./cbox-init.yaml` (current directory)

### Environment Variable Approach

```dockerfile
FROM php:8.3-fpm-alpine

COPY --from=cboxdk/init:latest \
    /usr/local/bin/cbox-init \
    /usr/local/bin/cbox-init

# Copy config to custom location
COPY cbox-init.yaml /app/config/cbox-init.yaml

# Set config path via environment
ENV CBOX_INIT_CONFIG=/app/config/cbox-init.yaml

ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

### Standard Location Approach

```dockerfile
FROM php:8.3-fpm-alpine

COPY --from=cboxdk/init:latest \
    /usr/local/bin/cbox-init \
    /usr/local/bin/cbox-init

# Copy to standard location
COPY cbox-init.yaml /etc/cbox-init/cbox-init.yaml

# No ENV needed - uses default location
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## Docker Compose Integration

### Basic Setup

```yaml
version: '3.8'

services:
  app:
    build: .
    ports:
      - "8080:80"
    environment:
      - APP_ENV=production
      - DB_HOST=db
    depends_on:
      - db
    restart: unless-stopped

  db:
    image: mysql:8
    environment:
      MYSQL_DATABASE: app
      MYSQL_ROOT_PASSWORD: secret
    volumes:
      - db-data:/var/lib/mysql

volumes:
  db-data:
```

### With Observability

```yaml
version: '3.8'

services:
  app:
    build: .
    ports:
      - "8080:80"
      - "9090:9090"  # Prometheus metrics
      - "8081:8080"  # Management API
    environment:
      - CBOX_INIT_GLOBAL_METRICS_ENABLED=true
      - CBOX_INIT_GLOBAL_API_ENABLED=true
      - CBOX_INIT_GLOBAL_API_AUTH=${API_TOKEN}
    restart: unless-stopped

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana-data:/var/lib/grafana

volumes:
  prometheus-data:
  grafana-data:
```

## Environment Variable Configuration

Override any configuration with environment variables:

```bash
docker run -d \
  -e CBOX_INIT_GLOBAL_LOG_LEVEL=debug \
  -e CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT=60 \
  -e CBOX_INIT_PROCESS_NGINX_ENABLED=true \
  -e CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=5 \
  my-app
```

### Environment Variable Format

```
CBOX_INIT_{SECTION}_{SUBSECTION}_{KEY}

Examples:
CBOX_INIT_GLOBAL_LOG_LEVEL          → global.log_level
CBOX_INIT_GLOBAL_METRICS_ENABLED    → global.metrics_enabled
CBOX_INIT_PROCESS_NGINX_ENABLED     → processes.nginx.enabled
CBOX_INIT_PROCESS_QUEUE_SCALE       → processes.queue.scale
```

## Signal Handling

Cbox Init handles signals appropriately as PID 1:

```bash
# Graceful shutdown
docker stop my-app
# Sends SIGTERM → Cbox Init gracefully shuts down processes

# Force shutdown
docker kill my-app
# Sends SIGKILL → Immediate termination

# Custom signal
docker kill -s SIGQUIT my-app
# Cbox Init initiates graceful shutdown
```

## Health Check Integration

Use Docker health checks with Cbox Init's management API:

```dockerfile
FROM php:8.3-fpm-alpine

# ... setup ...

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD wget -q -O- http://localhost:9180/api/v1/health || exit 1

ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

Or check processes directly:

```dockerfile
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD pgrep -f "php-fpm: master" && pgrep -f "nginx: master" || exit 1
```

## Resource Limits

Configure resource limits in Docker:

```yaml
services:
  app:
    build: .
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 1G
```

## Security Considerations

### Run as Non-Root User

```dockerfile
FROM php:8.3-fpm-alpine

# Create user
RUN addgroup -g 1000 app && \
    adduser -D -u 1000 -G app app

# ... setup ...

# Switch to non-root user
USER app

ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

### Read-Only Root Filesystem

```yaml
services:
  app:
    build: .
    read_only: true
    tmpfs:
      - /tmp
      - /var/run
      - /var/log/nginx
```

## Debugging

### View Cbox Init Logs

```bash
# Follow logs
docker logs -f my-app

# Filter for Cbox Init messages
docker logs my-app 2>&1 | grep "Cbox Init"

# Filter for specific process
docker logs my-app 2>&1 | grep "process=nginx"
```

### Inspect Running Processes

```bash
# List all processes
docker exec my-app ps aux

# Check process tree
docker exec my-app pstree -p 1

# View Cbox Init status via API
curl http://localhost:9180/api/v1/processes
```

### Debug Mode

```yaml
global:
  log_level: debug  # Enable debug logging
  log_format: text  # Human-readable logs
```

## Production Best Practices

1. **Always use specific version tags**
   ```dockerfile
   COPY --from=cboxdk/init:1.0.0 \
       /usr/local/bin/cbox-init \
       /usr/local/bin/cbox-init
   ```

2. **Enable health checks**
   - Use Docker HEALTHCHECK or
   - Configure Cbox Init health checks

3. **Configure graceful shutdown**
   ```yaml
   global:
     shutdown_timeout: 60  # Adjust based on workload
   ```

4. **Enable observability**
   ```yaml
   global:
     metrics_enabled: true
     api_enabled: true
   ```

5. **Use read-only filesystem when possible**

6. **Set resource limits**

7. **Run as non-root user**

## Next Steps

- [Configuration Overview](../configuration/overview) - Complete configuration reference
- [Health Checks](../features/health-checks) - Configure health monitoring
- [Prometheus Metrics](../observability/metrics) - Monitor with Prometheus
- [Examples](../examples/) - Real-world Dockerfiles

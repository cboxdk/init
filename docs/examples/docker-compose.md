---
title: "Docker Compose"
description: "Multi-container PHP deployment with Docker Compose, environment-specific profiles, and service orchestration"
weight: 35
---

# Docker Compose Example

Deploy PHP applications with Docker Compose using Cbox Init for process management and environment-specific configuration.

## Use Cases

- ✅ Local development with full stack
- ✅ Multi-environment deployments (dev/staging/prod)
- ✅ Microservices orchestration
- ✅ Testing with isolated environments
- ✅ CI/CD pipeline integration

## Basic Docker Compose Setup

### docker-compose.yml

```yaml
version: '3.8'

services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "80:80"
      - "9090:9090"  # Prometheus metrics
    environment:
      # PHP-FPM auto-tuning
      PHP_FPM_AUTOTUNE_PROFILE: "medium"

      # Application environment
      APP_ENV: production
      APP_KEY: ${APP_KEY}
      DB_HOST: database
      DB_DATABASE: app
      DB_USERNAME: app
      DB_PASSWORD: ${DB_PASSWORD}
      REDIS_HOST: redis

      # Cbox Init settings
      CBOX_INIT_GLOBAL_METRICS_ENABLED: "true"
      CBOX_INIT_GLOBAL_LOG_LEVEL: "info"
      CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE: "3"

    deploy:
      resources:
        limits:
          memory: 2G
          cpus: '2'

    depends_on:
      database:
        condition: service_healthy
      redis:
        condition: service_healthy

    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 30s

  database:
    image: mysql:8.0
    environment:
      MYSQL_DATABASE: app
      MYSQL_USER: app
      MYSQL_PASSWORD: ${DB_PASSWORD}
      MYSQL_ROOT_PASSWORD: ${DB_ROOT_PASSWORD}
    ports:
      - "3306:3306"
    volumes:
      - db-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 5s
      timeout: 3s
      retries: 5

  redis:
    image: redis:alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
    volumes:
      - redis-data:/data

volumes:
  db-data:
  redis-data:
```

### Dockerfile

```dockerfile
FROM php:8.3-fpm-alpine

# Install system dependencies
RUN apk add --no-cache \
    nginx \
    curl \
    mysql-client \
    redis

# Install PHP extensions
RUN docker-php-ext-install \
    pdo_mysql \
    opcache \
    pcntl \
    bcmath

# Install Composer
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer

# Copy application
WORKDIR /var/www/html
COPY . .

# Install dependencies
RUN composer install --no-dev --optimize-autoloader

# Copy Cbox Init
COPY --from=ghcr.io/cboxdk/init:latest /cbox-init /usr/local/bin/cbox-init

# Copy configurations
COPY docker/cbox-init.yaml /etc/cbox-init/cbox-init.yaml
COPY docker/nginx.conf /etc/nginx/nginx.conf

# Set permissions
RUN chown -R www-data:www-data /var/www/html

# Expose ports
EXPOSE 80 9090

# Run Cbox Init as PID 1
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## Multi-Environment Setup

### Development Environment

**docker-compose.dev.yml:**
```yaml
version: '3.8'

services:
  app:
    build:
      target: development
    environment:
      APP_ENV: local
      APP_DEBUG: "true"
      PHP_FPM_AUTOTUNE_PROFILE: "dev"
      CBOX_INIT_GLOBAL_LOG_LEVEL: "debug"
      CBOX_INIT_GLOBAL_LOG_FORMAT: "text"  # Human-readable
      CBOX_INIT_PROCESS_HORIZON_ENABLED: "false"
      CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE: "1"

    volumes:
      - ./:/var/www/html  # Mount source code for development

    deploy:
      resources:
        limits:
          memory: 512M
          cpus: '1'
```

**Run development:**
```bash
docker-compose -f docker-compose.yml -f docker-compose.dev.yml up
```

### Staging Environment

**docker-compose.staging.yml:**
```yaml
version: '3.8'

services:
  app:
    environment:
      APP_ENV: staging
      APP_DEBUG: "false"
      PHP_FPM_AUTOTUNE_PROFILE: "medium"
      CBOX_INIT_GLOBAL_LOG_LEVEL: "info"
      CBOX_INIT_GLOBAL_METRICS_ENABLED: "true"
      CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE: "3"

    deploy:
      resources:
        limits:
          memory: 2G
          cpus: '2'
```

### Production Environment

**docker-compose.prod.yml:**
```yaml
version: '3.8'

services:
  app:
    environment:
      APP_ENV: production
      APP_DEBUG: "false"
      PHP_FPM_AUTOTUNE_PROFILE: "heavy"
      CBOX_INIT_GLOBAL_LOG_LEVEL: "warn"
      CBOX_INIT_GLOBAL_LOG_REDACTION_ENABLED: "true"
      CBOX_INIT_GLOBAL_METRICS_ENABLED: "true"
      CBOX_INIT_GLOBAL_API_ENABLED: "true"
      CBOX_INIT_GLOBAL_API_AUTH: ${API_TOKEN}
      CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE: "10"

    deploy:
      resources:
        limits:
          memory: 8G
          cpus: '8'
      replicas: 3  # Docker Swarm mode
```

**Run production:**
```bash
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

## Service Dependencies

### Wait for Database

```yaml
services:
  app:
    depends_on:
      database:
        condition: service_healthy  # Wait for MySQL health check

  database:
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 5s
      retries: 10
```

**Alternative: Custom Wait Script**
```yaml
services:
  app:
    command: ["/wait-for-db.sh", "&&", "/usr/local/bin/cbox-init"]
```

**wait-for-db.sh:**
```bash
#!/bin/bash
until php artisan db:ping; do
    echo "Waiting for database..."
    sleep 2
done
echo "Database is ready!"
```

## Network Configuration

### Internal Network

```yaml
version: '3.8'

services:
  app:
    networks:
      - backend
      - frontend

  database:
    networks:
      - backend  # Only accessible to backend services

  nginx-lb:
    networks:
      - frontend  # Only accessible from frontend

networks:
  backend:
    internal: true  # No external access
  frontend:
    driver: bridge
```

### Service Discovery

```yaml
services:
  app:
    environment:
      # Services accessible by service name
      DB_HOST: database  # Resolves to database container
      REDIS_HOST: redis  # Resolves to redis container
      MAIL_HOST: mailhog  # Resolves to mailhog container
```

## Volume Management

### Named Volumes

```yaml
volumes:
  db-data:
    driver: local
  redis-data:
    driver: local
  app-storage:
    driver: local
    driver_opts:
      type: nfs
      o: addr=nfs-server,rw
      device: ":/path/to/storage"
```

### Bind Mounts (Development)

```yaml
services:
  app:
    volumes:
      - ./:/var/www/html              # Source code
      - ./docker/nginx.conf:/etc/nginx/nginx.conf
      - ./docker/cbox-init.yaml:/etc/cbox-init/cbox-init.yaml
```

## Scaling with Docker Compose

### Manual Scaling

```bash
# Scale queue workers to 5 instances
docker-compose up -d --scale queue-worker=5

# Scale down to 2
docker-compose up -d --scale queue-worker=2
```

### Swarm Mode Scaling

```yaml
version: '3.8'

services:
  app:
    deploy:
      replicas: 3
      update_config:
        parallelism: 1
        delay: 10s
      rollback_config:
        parallelism: 1
      resources:
        limits:
          memory: 2G
          cpus: '2'
```

```bash
# Deploy to swarm
docker stack deploy -c docker-compose.yml myapp

# Scale service
docker service scale myapp_app=5
```

## Monitoring Stack

```yaml
version: '3.8'

services:
  app:
    # ... Laravel app with Cbox Init

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_PASSWORD}
    volumes:
      - grafana-data:/var/lib/grafana

volumes:
  prometheus-data:
  grafana-data:
```

**prometheus.yml:**
```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'cbox-init'
    static_configs:
      - targets: ['app:9090']
```

## Complete Multi-Service Example

```yaml
version: '3.8'

services:
  # PHP application with Cbox Init
  app:
    build: .
    ports:
      - "80:80"
      - "9090:9090"
    environment:
      PHP_FPM_AUTOTUNE_PROFILE: "medium"
      APP_ENV: production
      APP_KEY: ${APP_KEY}
      DB_HOST: database
      REDIS_HOST: redis
      QUEUE_CONNECTION: redis
    depends_on:
      database:
        condition: service_healthy
      redis:
        condition: service_healthy
    volumes:
      - app-storage:/var/www/html/storage
    networks:
      - app-network

  # MySQL database
  database:
    image: mysql:8.0
    environment:
      MYSQL_DATABASE: app
      MYSQL_USER: app
      MYSQL_PASSWORD: ${DB_PASSWORD}
      MYSQL_ROOT_PASSWORD: ${DB_ROOT_PASSWORD}
    ports:
      - "3306:3306"
    volumes:
      - db-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 5s
      retries: 10
    networks:
      - app-network

  # Redis cache
  redis:
    image: redis:alpine
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      retries: 5
    networks:
      - app-network

  # Mailhog (email testing)
  mailhog:
    image: mailhog/mailhog:latest
    ports:
      - "1025:1025"  # SMTP
      - "8025:8025"  # Web UI
    networks:
      - app-network

  # Prometheus monitoring
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    networks:
      - app-network

  # Grafana dashboards
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_PASSWORD}
    volumes:
      - grafana-data:/var/lib/grafana
    networks:
      - app-network

volumes:
  db-data:
  redis-data:
  app-storage:
  prometheus-data:
  grafana-data:

networks:
  app-network:
    driver: bridge
```

## Running the Stack

### Development

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f app

# Run Artisan commands
docker-compose exec app php artisan migrate

# Access services
# - App: http://localhost
# - Mailhog: http://localhost:8025
# - Prometheus: http://localhost:9091
# - Grafana: http://localhost:3000
```

### Production

```bash
# Use production override
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# Check health
docker-compose ps

# View metrics
curl http://localhost:9090/metrics
```

## Environment Files

### .env

```bash
# Application
APP_KEY=base64:your-key-here
APP_ENV=production
APP_DEBUG=false

# Database
DB_PASSWORD=secure-password
DB_ROOT_PASSWORD=root-password

# Monitoring
GRAFANA_PASSWORD=admin-password

# Heartbeats
BACKUP_HEARTBEAT_URL=https://hc-ping.com/uuid
```

### .env.development

```bash
APP_ENV=local
APP_DEBUG=true
PHP_FPM_AUTOTUNE_PROFILE=dev
CBOX_INIT_GLOBAL_LOG_LEVEL=debug
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=1
```

### .env.production

```bash
APP_ENV=production
APP_DEBUG=false
PHP_FPM_AUTOTUNE_PROFILE=heavy
CBOX_INIT_GLOBAL_LOG_LEVEL=warn
CBOX_INIT_GLOBAL_LOG_REDACTION_ENABLED=true
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=10
```

## Multi-Profile Deployment

```yaml
version: '3.8'

services:
  # Development profile
  app-dev:
    build: .
    environment:
      PHP_FPM_AUTOTUNE_PROFILE: "dev"
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: '1'

  # Medium traffic profile
  app-medium:
    build: .
    environment:
      PHP_FPM_AUTOTUNE_PROFILE: "medium"
    deploy:
      resources:
        limits:
          memory: 2G
          cpus: '2'

  # Heavy traffic profile
  app-heavy:
    build: .
    environment:
      PHP_FPM_AUTOTUNE_PROFILE: "heavy"
    deploy:
      resources:
        limits:
          memory: 8G
          cpus: '8'
```

## Service Orchestration

### Startup Order

```yaml
services:
  # 1. Database starts first
  database:
    image: mysql:8.0
    healthcheck:
      test: ["CMD", "mysqladmin", "ping"]

  # 2. Redis starts in parallel with database
  redis:
    image: redis:alpine
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]

  # 3. App waits for both
  app:
    depends_on:
      database:
        condition: service_healthy
      redis:
        condition: service_healthy
```

**Startup flow:**
```
database + redis (parallel)
        ↓
  Both healthy
        ↓
    app starts
        ↓
Cbox Init runs pre-start hooks
        ↓
PHP-FPM → Nginx → Horizon → Queue
```

### Graceful Shutdown

```bash
# Stop services gracefully
docker-compose down

# Cbox Init handles:
# 1. Stop accepting new requests
# 2. Finish current jobs (Horizon)
# 3. Terminate queue workers
# 4. Stop Nginx
# 5. Stop PHP-FPM
```

## Volume Strategies

### Development

```yaml
services:
  app:
    volumes:
      # Hot reload for development
      - ./:/var/www/html
      - ./docker/cbox-init.yaml:/etc/cbox-init/cbox-init.yaml

      # Prevent node_modules from mounting
      - /var/www/html/node_modules
```

### Production

```yaml
services:
  app:
    volumes:
      # Only mount storage directory
      - app-storage:/var/www/html/storage

      # Read-only config
      - ./cbox-init.yaml:/etc/cbox-init/cbox-init.yaml:ro
```

## Monitoring Integration

### Prometheus + Grafana

**prometheus.yml:**
```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'cbox-init'
    static_configs:
      - targets:
          - app:9090
    metrics_path: /metrics
```

**Access dashboards:**
```bash
# Prometheus
http://localhost:9091

# Grafana
http://localhost:3000
# Login: admin / ${GRAFANA_PASSWORD}
```

### Log Aggregation

```yaml
services:
  app:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  # Alternative: Loki driver
  app:
    logging:
      driver: "loki"
      options:
        loki-url: "http://loki:3100/loki/api/v1/push"
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build image
        run: docker-compose build

      - name: Run tests
        run: docker-compose run --rm app php artisan test

      - name: Deploy
        run: |
          docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d

      - name: Health check
        run: |
          sleep 30
          curl -f http://localhost/health
```

### GitLab CI

```yaml
deploy:
  stage: deploy
  script:
    - docker-compose build
    - docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d
    - sleep 30
    - curl -f http://localhost/health
  only:
    - main
```

## Troubleshooting

### Services Not Starting in Order

**Problem:** App starts before database is ready

**Solution:** Use health check conditions
```yaml
depends_on:
  database:
    condition: service_healthy  # Don't just use 'depends_on'
```

### Port Conflicts

**Problem:** Port already in use

**Solution:** Change port mapping
```yaml
services:
  app:
    ports:
      - "8080:80"  # Map to 8080 instead of 80
```

### Memory Limits Not Working

**Problem:** Docker Compose doesn't enforce limits

**Solution:** Enable resource constraints
```bash
# Linux: Enable cgroup v2
docker-compose --compatibility up

# Or use Docker Swarm mode
docker stack deploy -c docker-compose.yml myapp
```

### Container OOM Killed

**Problem:** Container exceeds memory limit

**Solution:**
```yaml
services:
  app:
    environment:
      PHP_FPM_AUTOTUNE_PROFILE: "light"  # Reduce workers
    deploy:
      resources:
        limits:
          memory: 4G  # Increase limit
```

## Best Practices

### ✅ Do

**Use health checks:**
```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost/health"]
  interval: 10s
  retries: 3
```

**Set resource limits:**
```yaml
deploy:
  resources:
    limits:
      memory: 2G
      cpus: '2'
```

**Use named volumes:**
```yaml
volumes:
  - db-data:/var/lib/mysql  # Persists data
```

**Separate environments:**
```bash
docker-compose -f docker-compose.yml -f docker-compose.dev.yml up
```

### ❌ Don't

**Don't run as root:**
```dockerfile
# Add user in Dockerfile
RUN addgroup -g 1000 www && adduser -u 1000 -G www -s /bin/sh -D www
USER www
```

**Don't bind mount in production:**
```yaml
# ❌ Development only
volumes:
  - ./:/var/www/html

# ✅ Production
volumes:
  - app-storage:/var/www/html/storage
```

**Don't expose unnecessary ports:**
```yaml
# ❌ Exposes database publicly
database:
  ports:
    - "3306:3306"

# ✅ Only internal access
database:
  expose:
    - "3306"  # Only accessible to other services
```

## See Also

- [Docker Integration](../getting-started/docker-integration) - Dockerfile patterns
- [Kubernetes Deployment](kubernetes) - Kubernetes alternative
- [Laravel with Monitoring](laravel-with-monitoring) - Full observability stack
- [Environment Variables](../configuration/environment-variables) - ENV configuration

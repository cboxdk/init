---
title: "Laravel Complete Setup"
description: "Complete Laravel production stack with PHP-FPM, Nginx, Horizon, queue workers, and health checks"
weight: 31
---

# Laravel Complete Setup

Production-ready Laravel configuration with all services: PHP-FPM, Nginx, Horizon, Reverb, queue workers, and Laravel Scheduler.

## Use Cases

- ✅ Complete Laravel production deployments
- ✅ Multi-service orchestration with dependencies
- ✅ Queue processing with Horizon dashboard
- ✅ Real-time features with Reverb WebSockets
- ✅ Scheduled tasks with Laravel Scheduler
- ✅ Health monitoring and metrics

## Complete Configuration

**File:** `cbox-init.yaml`

```yaml
version: "1.0"

global:
  shutdown_timeout: 60
  log_format: json
  log_level: info
  metrics_enabled: true
  metrics_port: 9090

# Laravel optimization hooks
hooks:
  pre-start:
    - name: config-cache
      command: ["php", "artisan", "config:cache"]
      timeout: 60

    - name: route-cache
      command: ["php", "artisan", "route:cache"]
      timeout: 60

    - name: view-cache
      command: ["php", "artisan", "view:cache"]
      timeout: 120

    - name: migrate
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

    - name: storage-link
      command: ["php", "artisan", "storage:link"]
      timeout: 30
      continue_on_error: true

processes:
  # PHP-FPM - Core application runtime
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
    health_check:
      type: tcp
      address: 127.0.0.1:9000
      initial_delay: 5
      period: 10
      timeout: 3
      failure_threshold: 3
    shutdown:
      signal: SIGQUIT
      timeout: 30

  # Nginx - Web server
  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    restart: always
    depends_on: [php-fpm]
    health_check:
      type: http
      url: http://localhost/health
      initial_delay: 3
      period: 10
      timeout: 2
      failure_threshold: 3
      expected_status: 200
    shutdown:
      signal: SIGTERM
      timeout: 30

  # Laravel Horizon - Queue dashboard + workers
  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    restart: always
    depends_on: [php-fpm]
    health_check:
      type: exec
      command: ["php", "artisan", "horizon:status"]
      initial_delay: 10
      period: 30
      timeout: 5
      failure_threshold: 2
    shutdown:
      pre_stop_hook:
        name: horizon-terminate
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60
      signal: SIGTERM
      timeout: 120

  # Laravel Reverb - WebSocket server
  reverb:
    enabled: false  # Enable for real-time features
    command: ["php", "artisan", "reverb:start"]
    restart: always
    depends_on: [php-fpm]
    health_check:
      type: tcp
      address: 127.0.0.1:8080
      initial_delay: 5
      period: 15
      timeout: 3
    shutdown:
      pre_stop_hook:
        name: reverb-restart
        command: ["php", "artisan", "reverb:restart"]
        timeout: 30
      signal: SIGTERM
      timeout: 60

  # Queue workers - Default queue
  queue-default:
    enabled: true
    command: ["php", "artisan", "queue:work", "--queue=default", "--tries=3", "--max-time=3600"]
    scale: 2
    restart: on-failure
    depends_on: [php-fpm]
    shutdown:
      signal: SIGTERM
      timeout: 60

  # Laravel Scheduler - Cron replacement
  scheduler:
    enabled: true
    command: ["php", "artisan", "schedule:run"]
    schedule: "* * * * *"  # Every minute
    restart: never
    depends_on: [php-fpm]
```

## Architecture Overview

```
┌─────────────────────────────────────────────────┐
│  Container (Cbox Init as PID 1)                 │
│                                                  │
│  Pre-Start Hooks:                               │
│   1. config:cache ─> 2. route:cache ─>         │
│   3. view:cache ─> 4. migrate ─> 5. storage    │
│                                                  │
│  ┌─────────────┐                                │
│  │  PHP-FPM    │ (Priority 10, TCP health)      │
│  │  :9000      │                                │
│  └──────┬──────┘                                │
│         │                                        │
│         ├──> ┌─────────────┐                    │
│         │    │   Nginx     │ (Priority 20)      │
│         │    │   :80       │ (HTTP health)      │
│         │    └─────────────┘                    │
│         │                                        │
│         ├──> ┌─────────────┐                    │
│         │    │   Horizon   │ (Priority 30)      │
│         │    │             │ (Exec health)      │
│         │    └─────────────┘                    │
│         │                                        │
│         ├──> ┌─────────────┐                    │
│         │    │   Reverb    │ (Priority 30)      │
│         │    │   :8080     │ (TCP health)       │
│         │    └─────────────┘                    │
│         │                                        │
│         └──> ┌─────────────┐                    │
│              │ Queue ×2    │ (Priority 40)      │
│              │ Workers     │ (Scaled)           │
│              └─────────────┘                    │
│                                                  │
│         ┌─────────────┐                         │
│         │ Scheduler   │ (Cron: * * * * *)      │
│         └─────────────┘                         │
└─────────────────────────────────────────────────┘
```

## Configuration Walkthrough

### Pre-Start Hooks

Optimize Laravel before starting processes:

```yaml
hooks:
  pre-start:
    # Cache configuration files
    - name: config-cache
      command: ["php", "artisan", "config:cache"]
      timeout: 60

    # Cache routes
    - name: route-cache
      command: ["php", "artisan", "route:cache"]
      timeout: 60

    # Cache Blade views
    - name: view-cache
      command: ["php", "artisan", "view:cache"]
      timeout: 120

    # Run database migrations
    - name: migrate
      command: ["php", "artisan", "migrate", "--force"]
      timeout: 300

    # Create storage symlink
    - name: storage-link
      command: ["php", "artisan", "storage:link"]
      timeout: 30
      continue_on_error: true  # OK if already exists
```

**Why these hooks:**
- **config:cache** - Speeds up config loading by 2-3x
- **route:cache** - Dramatically faster routing (10-50x)
- **view:cache** - Pre-compile Blade templates
- **migrate** - Ensure database schema is current
- **storage:link** - Create public storage symlink

### PHP-FPM Configuration

```yaml
php-fpm:
  enabled: true
  command: ["php-fpm", "-F", "-R"]
  restart: always

  health_check:
    type: tcp
    address: 127.0.0.1:9000  # PHP-FPM FastCGI port
    initial_delay: 5
    period: 10
    timeout: 3
    failure_threshold: 3

  shutdown:
    signal: SIGQUIT  # Graceful shutdown for PHP-FPM
    timeout: 30
```

**Key points:**
- TCP health check on port 9000 (FastCGI)
- SIGQUIT for graceful shutdown (vs SIGTERM)
- 30-second timeout to finish active requests

### Nginx Configuration

```yaml
nginx:
  enabled: true
  command: ["nginx", "-g", "daemon off;"]
  depends_on: [php-fpm]  # Wait for PHP-FPM health

  health_check:
    type: http
    url: http://localhost/health
    expected_status: 200
    period: 10
```

**Dependency behavior:**
- Waits for PHP-FPM to pass health checks
- Prevents "502 Bad Gateway" errors on startup
- Ensures FastCGI backend is ready

**Health endpoint:**
```php
// routes/web.php
Route::get('/health', function () {
    return response()->json(['status' => 'healthy'], 200);
});
```

### Laravel Horizon Configuration

```yaml
horizon:
  enabled: true
  command: ["php", "artisan", "horizon"]
  depends_on: [php-fpm]

  shutdown:
    pre_stop_hook:
      command: ["php", "artisan", "horizon:terminate"]
      timeout: 60  # Wait for terminate signal
    timeout: 120  # Total shutdown time (finish jobs)
```

**Graceful shutdown flow:**
1. `horizon:terminate` sends termination signal
2. Horizon stops accepting new jobs
3. Currently running jobs finish
4. Horizon exits cleanly
5. If timeout (120s) expires, SIGTERM is sent

### Queue Worker Configuration

```yaml
queue-default:
  enabled: true
  command: ["php", "artisan", "queue:work", "--queue=default", "--tries=3", "--max-time=3600"]
  scale: 2  # Run 2 workers
  restart: on-failure  # Only restart on errors
  depends_on: [php-fpm]

  shutdown:
    signal: SIGTERM
    timeout: 60  # Finish current job
```

**Worker arguments:**
- `--queue=default` - Process default queue
- `--tries=3` - Retry failed jobs 3 times
- `--max-time=3600` - Restart worker after 1 hour

**Scaling:**
- `scale: 2` creates `queue-default-1` and `queue-default-2`
- Adjust based on queue depth and processing time
- Typical range: 2-10 workers

### Laravel Scheduler

```yaml
scheduler:
  enabled: true
  command: ["php", "artisan", "schedule:run"]
  schedule: "* * * * *"  # Every minute
  restart: never
```

**How it works:**
- Runs `php artisan schedule:run` every minute
- Laravel's internal scheduler handles task timing
- Replaces cron in containers

**Heartbeat monitoring:**
```yaml
scheduler:
  heartbeat:
    failure_url: https://hc-ping.com/uuid/fail
```

## Startup Sequence

```
1. Pre-Start Hooks (sequential)
   └─> config:cache → route:cache → view:cache → migrate → storage:link

2. PHP-FPM (priority 10)
   └─> Waits for TCP health check on :9000

3. Nginx (priority 20)
   └─> Waits for PHP-FPM health, then starts
   └─> HTTP health check on :80/health

4. Horizon + Reverb (priority 30, parallel)
   └─> Both wait for PHP-FPM health
   └─> Start simultaneously

5. Queue Workers (priority 40)
   └─> Waits for PHP-FPM health
   └─> 2 instances start: queue-default-1, queue-default-2

6. Scheduler (priority 50)
   └─> Waits for PHP-FPM health
   └─> Runs every minute via cron schedule
```

## Dockerfile Integration

```dockerfile
FROM php:8.3-fpm-alpine

# Install system dependencies
RUN apk add --no-cache \
    nginx \
    supervisor \
    curl

# Install PHP extensions
RUN docker-php-ext-install pdo_mysql opcache

# Copy application
WORKDIR /var/www/html
COPY . .

# Install dependencies
RUN composer install --no-dev --optimize-autoloader

# Copy Cbox Init
COPY --from=ghcr.io/cboxdk/init:latest /cbox-init /usr/local/bin/cbox-init

# Copy configuration
COPY cbox-init.yaml /etc/cbox-init/cbox-init.yaml

# Copy Nginx config
COPY nginx.conf /etc/nginx/nginx.conf

# Expose ports
EXPOSE 80 9090

# Environment defaults
ENV PHP_FPM_AUTOTUNE_PROFILE=medium \
    CBOX_INIT_GLOBAL_LOG_FORMAT=json \
    CBOX_INIT_GLOBAL_METRICS_ENABLED=true

# Run Cbox Init as PID 1
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## Nginx Configuration

**File:** `nginx.conf`

```nginx
worker_processes auto;
error_log /dev/stderr warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format json escape=json
        '{'
            '"time":"$time_iso8601",'
            '"request":"$request",'
            '"status":$status,'
            '"bytes":$body_bytes_sent,'
            '"duration":$request_time,'
            '"ip":"$remote_addr"'
        '}';

    access_log /dev/stdout json;

    sendfile on;
    keepalive_timeout 65;

    upstream php-fpm {
        server 127.0.0.1:9000;
    }

    server {
        listen 80;
        server_name _;
        root /var/www/html/public;
        index index.php;

        location / {
            try_files $uri $uri/ /index.php?$query_string;
        }

        location ~ \.php$ {
            fastcgi_pass php-fpm;
            fastcgi_index index.php;
            fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
            include fastcgi_params;
        }

        location /health {
            access_log off;
            return 200 "healthy\n";
            add_header Content-Type text/plain;
        }
    }
}
```

## Running the Stack

### Build and Run

```bash
# Build Docker image
docker build -t laravel-app:latest .

# Run with auto-tuning
docker run -d \
  --name laravel \
  -p 80:80 \
  -p 9090:9090 \
  -e PHP_FPM_AUTOTUNE_PROFILE=medium \
  -e DB_HOST=host.docker.internal \
  -m 2G \
  --cpus 2 \
  laravel-app:latest
```

### Docker Compose

```yaml
version: '3.8'

services:
  app:
    build: .
    ports:
      - "80:80"
      - "9090:9090"  # Prometheus metrics
    environment:
      PHP_FPM_AUTOTUNE_PROFILE: "medium"
      CBOX_INIT_GLOBAL_METRICS_ENABLED: "true"
      CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE: "3"

      # Laravel environment
      APP_ENV: production
      APP_KEY: ${APP_KEY}
      DB_CONNECTION: mysql
      DB_HOST: database
      REDIS_HOST: redis

    deploy:
      resources:
        limits:
          memory: 2G
          cpus: '2'
    depends_on:
      - database
      - redis

  database:
    image: mysql:8.0
    environment:
      MYSQL_DATABASE: laravel
      MYSQL_ROOT_PASSWORD: secret
    volumes:
      - db-data:/var/lib/mysql

  redis:
    image: redis:alpine

volumes:
  db-data:
```

## Service Breakdown

### 1. PHP-FPM (Priority 10)

**Purpose:** Core PHP runtime for handling web requests

**Configuration:**
```yaml
php-fpm:
  command: ["php-fpm", "-F", "-R"]  # Foreground, allow root
  health_check:
    type: tcp
    address: 127.0.0.1:9000  # FastCGI port
```

**Auto-Tuning:**
```bash
# With 2GB container and medium profile
# Calculated: ~16 workers (CPU limited)
PHP_FPM_AUTOTUNE_PROFILE=medium
```

See [PHP-FPM Auto-Tuning](../php-fpm-autotune) for worker optimization.

### 2. Nginx (Priority 20)

**Purpose:** Web server, reverse proxy to PHP-FPM

**Configuration:**
```yaml
nginx:
  depends_on: [php-fpm]  # Wait for PHP-FPM
  health_check:
    type: http
    url: http://localhost/health
    expected_status: 200
```

**Why depends_on:**
- Prevents "502 Bad Gateway" during startup
- Nginx starts only after PHP-FPM is healthy
- Avoids race conditions

### 3. Laravel Horizon (Priority 30)

**Purpose:** Queue dashboard and supervised queue workers

**Configuration:**
```yaml
horizon:
  shutdown:
    pre_stop_hook:
      command: ["php", "artisan", "horizon:terminate"]
      timeout: 60
    timeout: 120  # Total shutdown time
```

**Graceful shutdown:**
- Sends terminate signal via Artisan command
- Allows current jobs to complete
- Maximum 120 seconds before force-kill

**Access dashboard:**
```
http://your-app.com/horizon
```

### 4. Laravel Reverb (Priority 30)

**Purpose:** Real-time WebSocket server for broadcasting

**When to enable:**
```yaml
reverb:
  enabled: true  # Enable for:
  # - Real-time notifications
  # - Live chat features
  # - Collaborative editing
  # - Live dashboards
```

**Configuration:**
```php
// config/broadcasting.php
'reverb' => [
    'driver' => 'reverb',
    'host' => env('REVERB_SERVER_HOST', '0.0.0.0'),
    'port' => env('REVERB_SERVER_PORT', 8080),
],
```

### 5. Queue Workers (Priority 40)

**Purpose:** Background job processing

**Scaling:**
```yaml
queue-default:
  scale: 2  # Creates queue-default-1, queue-default-2
```

**Adjust scale based on:**
- Queue depth (jobs waiting)
- Job processing time
- Available resources

**Monitoring:**
```bash
# Via Horizon dashboard
http://your-app.com/horizon

# Via Prometheus metrics
curl http://localhost:9090/metrics | grep queue
```

### 6. Laravel Scheduler (Priority 50)

**Purpose:** Run scheduled tasks (replaces cron)

**Configuration:**
```yaml
scheduler:
  command: ["php", "artisan", "schedule:run"]
  schedule: "* * * * *"  # Every minute
```

**How it works:**
- Runs every minute via cron schedule
- Laravel determines which tasks to execute
- Tasks defined in `app/Console/Kernel.php`

**Example Kernel:**
```php
protected function schedule(Schedule $schedule)
{
    $schedule->command('backup:run')->daily();
    $schedule->command('emails:send')->hourly();
}
```

## Environment Configuration

### Development

```bash
# .env.development
PHP_FPM_AUTOTUNE_PROFILE=dev
CBOX_INIT_GLOBAL_LOG_LEVEL=debug
CBOX_INIT_PROCESS_HORIZON_ENABLED=false
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=1
CBOX_INIT_PROCESS_SCHEDULER_ENABLED=false
```

### Staging

```bash
# .env.staging
PHP_FPM_AUTOTUNE_PROFILE=medium
CBOX_INIT_GLOBAL_LOG_LEVEL=info
CBOX_INIT_GLOBAL_METRICS_ENABLED=true
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=3
```

### Production

```bash
# .env.production
PHP_FPM_AUTOTUNE_PROFILE=heavy
CBOX_INIT_GLOBAL_LOG_LEVEL=warn
CBOX_INIT_GLOBAL_LOG_REDACTION_ENABLED=true
CBOX_INIT_GLOBAL_METRICS_ENABLED=true
CBOX_INIT_GLOBAL_API_ENABLED=true
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=10
```

## Monitoring

### Prometheus Metrics

```bash
# Check metrics endpoint
curl http://localhost:9090/metrics

# Key metrics:
# - cbox_init_process_up{process="php-fpm"}
# - cbox_init_process_restarts_total{process="nginx"}
# - cbox_init_process_health_status{process="horizon"}
```

### Health Status

```bash
# Check all process health
curl http://localhost:9180/api/v1/processes | jq '.[] | {name, health_status, state}'
```

## Troubleshooting

### Nginx 502 Bad Gateway

**Symptom:** Nginx starts before PHP-FPM is ready

**Solution:** Already configured with `depends_on: [php-fpm]`

**Verify:**
```bash
# Check startup order in logs
docker logs laravel-app | grep "started successfully"

# Should see:
# php-fpm started successfully
# nginx started successfully (after php-fpm)
```

### Horizon Won't Terminate

**Symptom:** Horizon doesn't stop gracefully, gets force-killed

**Solution:** Increase shutdown timeout

```yaml
horizon:
  shutdown:
    timeout: 300  # 5 minutes for long-running jobs
```

### Queue Workers Restarting

**Symptom:** Queue workers restart frequently

**Solution:** Check memory usage

```yaml
queue-default:
  command: ["php", "artisan", "queue:work", "--max-jobs=100"]  # Restart after 100 jobs
```

**Or increase container memory:**
```bash
docker run -m 4G laravel-app  # Was 2G
```

### Migrations Timeout

**Symptom:** Pre-start migrate hook times out

**Solution:** Increase timeout

```yaml
hooks:
  pre-start:
    - name: migrate
      timeout: 600  # 10 minutes for large migrations
```

## Performance Tuning

### PHP-FPM Workers

```bash
# Optimize based on container size
PHP_FPM_AUTOTUNE_PROFILE=medium  # 2GB container
PHP_FPM_AUTOTUNE_PROFILE=heavy   # 4-8GB container
```

### Queue Worker Count

```bash
# Start with 1 worker per CPU core
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=2  # 2 CPUs

# Scale up based on queue depth
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=5  # High traffic
```

### Horizon vs Queue Workers

**Use Horizon when:**
- Need queue dashboard and monitoring
- Want auto-balancing across queues
- Managing complex queue configurations

**Use queue:work when:**
- Simple queue processing
- Lower resource overhead
- Fine-grained control per queue

## See Also

- [PHP-FPM Auto-Tuning](../php-fpm-autotune) - Worker optimization
- [Health Checks](../configuration/health-checks) - Health monitoring
- [Lifecycle Hooks](../configuration/lifecycle-hooks) - Pre/post hooks
- [Process Scaling](../features/process-scaling) - Scale workers dynamically
- [Prometheus Metrics](../observability/metrics) - Monitoring guide

---
title: "Configuration Scaffolding"
description: "Generate production-ready configurations with scaffolding presets for Laravel, Symfony, and more"
weight: 27
---

# Configuration Scaffolding

PHPeek PM includes powerful scaffolding tools to quickly generate production-ready configuration files. Instead of writing configs from scratch, use presets for common PHP frameworks and deployment scenarios.

## Quick Start

```bash
# Generate Laravel configuration
./phpeek-pm scaffold laravel

# With Docker files
./phpeek-pm scaffold laravel --dockerfile --docker-compose

# Interactive mode (guided prompts)
./phpeek-pm scaffold --interactive

# Specify output directory
./phpeek-pm scaffold laravel --output ./docker --docker-compose --observability
```

## Available Presets

### PHP Frameworks

| Preset | Framework | Services | Best For |
|--------|-----------|----------|----------|
| **`laravel`** | Laravel | PHP-FPM, Nginx, Horizon, Queue Workers, Scheduler | Full Laravel apps with queues |
| **`symfony`** | Symfony | PHP-FPM, Nginx, Queue Workers | Symfony apps with Messenger |
| **`php`** | Any PHP | PHP-FPM, Nginx | Vanilla PHP apps, simple PHP sites |

> **Tip:** Add `--observability` to any preset for tracing, metrics, and API.

### CMS Frameworks

| Preset | Framework | Services | Best For |
|--------|-----------|----------|----------|
| **`wordpress`** | WordPress | PHP-FPM, Nginx, WP-CLI cron | WordPress sites with proper cron |
| **`magento`** | Magento 2 | PHP-FPM, Nginx, Queue consumers, Cron, Indexer | Magento e-commerce sites |
| **`drupal`** | Drupal | PHP-FPM, Nginx, Drush cron | Drupal CMS sites |

### Node.js Frameworks

| Preset | Framework | Services | Best For |
|--------|-----------|----------|----------|
| **`nextjs`** | Next.js | Node.js (standalone), Nginx reverse proxy | Next.js with SSR |
| **`nuxt`** | Nuxt 3 | Nitro server, Nginx reverse proxy | Nuxt 3 applications |
| **`nodejs`** | Node.js | Node.js server, Workers, Nginx reverse proxy | Express, Fastify, NestJS |

## Preset Details

### 1. Laravel (Full Stack)

**Complete Laravel application with all essential services.**

```bash
./phpeek-pm scaffold laravel --output ./config
```

**Includes:**
- ✅ **PHP-FPM** - TCP health check on port 9000
- ✅ **Nginx** - HTTP health check with PHP-FPM dependency
- ✅ **Horizon** - Redis queue manager with graceful shutdown hook
- ✅ **Queue Workers** - 3 instances with retry logic (`--tries=3`)
- ✅ **Scheduler** - Laravel cron replacement
- ✅ **API + Metrics** - Enabled by default

**Generated config highlights:**
```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    shutdown:
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60

  queue-default:
    enabled: true
    command: ["php", "artisan", "queue:work", "--tries=3"]
    scale: 3
```

### 2. Observability Stack (--observability flag)

**Add full observability to any preset with a single flag.**

```bash
# Add observability to any preset
./phpeek-pm scaffold laravel --observability
./phpeek-pm scaffold wordpress --observability
./phpeek-pm scaffold magento --observability --docker-compose
./phpeek-pm scaffold nextjs --observability --dockerfile --nginx
```

**The `--observability` flag enables:**
- ✅ **Distributed Tracing** - OpenTelemetry with OTLP gRPC
- ✅ **Prometheus Metrics** - Full metrics collection
- ✅ **Management API** - REST API for process control
- ✅ **Log Level** - Set to `warn` (reduced noise in production)

**Generated observability config:**
```yaml
global:
  log_level: warn
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: tempo:4317
  tracing_sample_rate: 0.1
  metrics_enabled: true
  api_enabled: true
```


### 3. Symfony

**Symfony application with Messenger queue workers.**

```bash
./phpeek-pm scaffold symfony --output ./config
```

**Includes:**
- ✅ **PHP-FPM** - FastCGI process manager
- ✅ **Nginx** - Web server with health checks
- ✅ **Queue Workers** - Symfony Messenger consumers
- ✅ **API + Metrics** - Enabled by default

**Generated config:**
```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  messenger-async:
    enabled: true
    command: ["php", "bin/console", "messenger:consume", "async", "--time-limit=3600"]
    scale: 2
    restart: always
```

### 4. PHP (Vanilla)

**Basic PHP application with PHP-FPM and Nginx.**

```bash
./phpeek-pm scaffold php --output ./config
```

**Includes:**
- ✅ **PHP-FPM** - FastCGI process manager
- ✅ **Nginx** - Web server with PHP-FPM dependency
- ✅ **API + Metrics** - Enabled by default

**Use cases:**
- Simple PHP applications without framework
- Custom PHP setups
- Legacy PHP applications

### 5. WordPress

**WordPress application with WP-CLI cron replacement.**

```bash
./phpeek-pm scaffold wordpress --output ./config --dockerfile
```

**Includes:**
- ✅ **PHP-FPM** - FastCGI process manager with health checks
- ✅ **Nginx** - Web server with PHP-FPM dependency
- ✅ **WP-CLI Cron** - Proper cron via `wp cron event run --due-now`
- ✅ **API + Metrics** - Enabled by default

**Generated config:**
```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  wp-cron:
    enabled: true
    command: ["wp", "cron", "event", "run", "--due-now"]
    type: oneshot
    restart: never
    schedule: "*/1 * * * *"
```

**Why WP-CLI cron?**
- WordPress's default `wp-cron.php` runs on page load, which is unreliable
- WP-CLI cron runs scheduled tasks independently of visitor traffic
- Ensure `define('DISABLE_WP_CRON', true);` in `wp-config.php`

### 6. Magento 2

**Magento 2 application with queue consumers, cron, and indexer.**

```bash
./phpeek-pm scaffold magento --output ./config --dockerfile --docker-compose
```

**Includes:**
- ✅ **PHP-FPM** - FastCGI process manager with health checks
- ✅ **Nginx** - Web server with PHP-FPM dependency
- ✅ **Queue Consumers** - 2 instances of async message processing
- ✅ **Cron** - Magento cron runner (every minute)
- ✅ **Indexer** - Reindex scheduler (every 15 minutes)
- ✅ **API + Metrics** - Enabled by default

**Generated config:**
```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  queue-consumer:
    enabled: true
    command: ["php", "bin/magento", "queue:consumers:start", "--max-messages=1000"]
    scale: 2
    restart: always
    depends_on: [php-fpm]

  cron:
    enabled: true
    command: ["php", "bin/magento", "cron:run"]
    type: oneshot
    restart: never
    schedule: "* * * * *"

  indexer:
    enabled: true
    command: ["php", "bin/magento", "indexer:reindex"]
    type: oneshot
    restart: never
    schedule: "*/15 * * * *"
```

**Magento-specific considerations:**
- Queue consumers handle async operations (order processing, notifications)
- Cron handles scheduled tasks (catalog updates, cache cleanup)
- Indexer keeps product/category data optimized for search
- Consider increasing `queue-consumer` scale for high-traffic stores

### 7. Drupal

**Drupal CMS with Drush cron.**

```bash
./phpeek-pm scaffold drupal --output ./config --dockerfile
```

**Includes:**
- ✅ **PHP-FPM** - FastCGI process manager with health checks
- ✅ **Nginx** - Web server with PHP-FPM dependency
- ✅ **Drush Cron** - Drupal cron via Drush command
- ✅ **API + Metrics** - Enabled by default

**Generated config:**
```yaml
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]

  drush-cron:
    enabled: true
    command: ["drush", "cron"]
    type: oneshot
    restart: never
    schedule: "*/5 * * * *"
```

**Drupal-specific considerations:**
- Drush cron runs scheduled tasks reliably outside of web requests
- 5-minute intervals balance freshness vs. server load
- Ensure Drush is installed and accessible in PATH

### 8. Next.js

**Next.js application with standalone output mode.**

```bash
./phpeek-pm scaffold nextjs --output ./config --dockerfile --nginx
```

**Includes:**
- ✅ **Node.js Server** - Standalone mode with `scale: 2` instances
- ✅ **Nginx** - Reverse proxy with upstream load balancing
- ✅ **Memory Protection** - `max_memory_mb: 512` for leak protection
- ✅ **Port Assignment** - Automatic `PORT` env var via `port_base: 3000`
- ✅ **API + Metrics** - Enabled by default

**Generated config:**
```yaml
processes:
  nextjs:
    command: ["node", ".next/standalone/server.js"]
    scale: 2
    port_base: 3000
    max_memory_mb: 512
    env:
      NODE_ENV: production
      HOSTNAME: "0.0.0.0"
    health_check:
      type: http
      url: "http://127.0.0.1:3000/api/health"

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [nextjs]
```

**Generated nginx.conf (with `--nginx`):**
```nginx
upstream nodejs_backend {
    least_conn;
    server 127.0.0.1:3000;
    server 127.0.0.1:3001;
}
```

**Requirements:**
- Add `output: 'standalone'` to `next.config.js`
- Implement `/api/health` endpoint for health checks

### 9. Nuxt 3

**Nuxt 3 application with Nitro server.**

```bash
./phpeek-pm scaffold nuxt --output ./config --dockerfile --nginx
```

**Includes:**
- ✅ **Nitro Server** - `.output/server/index.mjs` with `scale: 2`
- ✅ **Nginx** - Reverse proxy with upstream load balancing
- ✅ **Memory Protection** - `max_memory_mb: 512`
- ✅ **Port Assignment** - Automatic via `port_base: 3000`
- ✅ **API + Metrics** - Enabled by default

**Generated config:**
```yaml
processes:
  nuxt:
    command: ["node", ".output/server/index.mjs"]
    scale: 2
    port_base: 3000
    max_memory_mb: 512
    env:
      NODE_ENV: production
      NITRO_HOST: "0.0.0.0"
```

### 10. Generic Node.js

**Generic Node.js application with background workers.**

```bash
./phpeek-pm scaffold nodejs --output ./config --dockerfile --nginx
```

**Includes:**
- ✅ **App Server** - Main Node.js application with `scale: 2`
- ✅ **Workers** - Background job processors (BullMQ, etc.)
- ✅ **Nginx** - Reverse proxy with upstream load balancing
- ✅ **Memory Protection** - `max_memory_mb: 512` for app, 256MB for workers
- ✅ **API + Metrics** - Enabled by default

**Generated config:**
```yaml
processes:
  app:
    command: ["node", "dist/server.js"]
    scale: 2
    port_base: 3000
    max_memory_mb: 512
    env:
      NODE_ENV: production

  worker:
    command: ["node", "dist/worker.js"]
    scale: 2
    max_memory_mb: 256
    depends_on: [app]
```

**Use cases:**
- Express, Fastify, Koa applications
- NestJS backends
- Custom Node.js servers with background workers

## CLI Flags

### Required

```bash
phpeek-pm scaffold <preset>
```

**Available presets:**

PHP Frameworks:
- `laravel` - Full Laravel stack
- `production` - Laravel with observability
- `symfony` - Symfony application
- `generic` - Basic web server
- `minimal` - Empty template

CMS Frameworks:
- `wordpress` - WordPress with WP-CLI cron
- `magento` - Magento 2 with queue consumers
- `drupal` - Drupal with Drush cron

Node.js Frameworks:
- `nextjs` - Next.js (standalone mode)
- `nuxt` - Nuxt 3 (Nitro server)
- `nodejs` - Generic Node.js with workers

### Optional Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--interactive`, `-i` | Interactive mode with prompts | `false` |
| `--output PATH`, `-o PATH` | Output directory | Current directory |
| `--dockerfile` | Generate Dockerfile | `false` |
| `--docker-compose` | Generate docker-compose.yml | `false` |
| `--nginx` | Generate nginx.conf (with load balancing for Node.js) | `false` |
| `--observability` | Enable observability stack (tracing, metrics, API) | `false` |
| `--app-name STRING` | Application name | `my-app` |
| `--queue-workers INT` | Number of queue workers | `3` |

### Examples

```bash
# Basic usage
phpeek-pm scaffold laravel

# Customize application name and workers
phpeek-pm scaffold laravel --app-name api-service --queue-workers 5

# Generate with Docker files
phpeek-pm scaffold laravel --dockerfile --docker-compose --output ./docker

# Production setup with full observability (tracing + metrics + API)
phpeek-pm scaffold laravel --observability --docker-compose

# Any preset with observability
phpeek-pm scaffold wordpress --observability
phpeek-pm scaffold magento --observability --docker-compose
phpeek-pm scaffold nextjs --observability --dockerfile --nginx
```

## Interactive Mode

**Guided configuration with prompts.**

```bash
./phpeek-pm scaffold --interactive
```

### Prompt Flow

**1. Preset Selection**
```
Select a preset:

  PHP Frameworks:
    1.  laravel     - Laravel application (full stack)
    2.  symfony     - Symfony application
    3.  wordpress   - WordPress with WP-CLI cron
    4.  magento     - Magento 2 with queue consumers
    5.  drupal      - Drupal with Drush cron
    6.  generic     - Generic PHP application
    7.  minimal     - Minimal setup (PHP-FPM only)
    8.  production  - Production-ready Laravel (all features)

  Node.js Frameworks:
    9.  nextjs      - Next.js application (standalone mode)
    10. nuxt        - Nuxt 3 application (Nitro server)
    11. nodejs      - Generic Node.js application

Enter choice [1-11]: 1
```

**2. Application Name**
```
Application name [my-app]: api-service
```

**3. Log Level**
```
Log level [info]:
  1. debug
  2. info
  3. warn
  4. error

Enter choice [1-4]: 2
```

**4. Queue Workers** (Laravel/Symfony only)
```
Number of queue workers [3]: 5
```

**5. Queue Connection** (Laravel only)
```
Queue connection [redis]:
  1. redis
  2. database
  3. sqs

Enter choice [1-3]: 1
```

**6. Feature Toggles**
```
Enable Prometheus metrics? [y/n]: y
Enable Management API? [y/n]: y
Enable distributed tracing? [n]: n
```

**7. Docker Files**
```
Generate docker-compose.yml? [n]: y
Generate Dockerfile? [n]: n
```

**8. Confirmation**
```
Configuration:
  Preset: Laravel
  App Name: api-service
  Workers: 5
  Metrics: Enabled
  API: Enabled

Generate configuration? [y/n]: y

✅ Files generated:
   - phpeek-pm.yaml
   - docker-compose.yml
```

## Generated Files

### phpeek-pm.yaml

**Main configuration file** with process definitions, health checks, and global settings.

**Contents:**
- Process orchestration with priorities
- Health checks (TCP/HTTP)
- Graceful shutdown hooks
- Resource scaling
- Logging configuration
- Optional observability (metrics, tracing, API)

**Example structure:**
```yaml
version: "1.0"

global:
  shutdown_timeout: 30
  log_format: json
  log_level: info
  metrics_enabled: true
  api_enabled: true

processes:
  php-fpm:
    enabled: true
    # ... full process config
```

### docker-compose.yml

**Generated with `--docker-compose` flag.**

**Includes:**
- Application container with PHPeek PM
- MySQL 8.0 database
- Redis cache/queue
- Prometheus metrics (production preset)
- Grafana dashboards (production preset)
- Network configuration
- Volume mounts
- Port mappings (80, 443, 8080, 9090)

**Example:**
```yaml
version: '3.8'

services:
  app:
    build: .
    ports:
      - "80:80"
      - "443:443"
      - "8080:8080"
    environment:
      - PHP_FPM_AUTOTUNE_PROFILE=medium
    volumes:
      - ./:/var/www/html
    depends_on:
      - mysql
      - redis

  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: secret
      MYSQL_DATABASE: myapp

  redis:
    image: redis:7-alpine

  prometheus:  # Production preset only
    image: prom/prometheus:latest
    # ... full Prometheus config

  grafana:  # Production preset only
    image: grafana/grafana:latest
    # ... full Grafana config
```

### Dockerfile

**Generated with `--dockerfile` flag.**

**Multi-stage PHP 8.2 Docker image:**
- Base: Official PHP-FPM with extensions
- Dependencies: Composer packages + system libraries
- PHPeek PM binary
- Optimized layers with caching
- Production-ready settings
- Health check integration

**Example:**
```dockerfile
FROM php:8.2-fpm-alpine AS base

# Install PHP extensions
RUN apk add --no-cache \
    postgresql-dev \
    && docker-php-ext-install pdo pdo_pgsql opcache

# Copy PHPeek PM binary
COPY --from=builder /app/phpeek-pm /usr/local/bin/phpeek-pm

# Copy application
WORKDIR /var/www/html
COPY . .

# Install Composer dependencies
RUN composer install --no-dev --optimize-autoloader

# Configure PHP-FPM
COPY php-fpm.conf /usr/local/etc/php-fpm.d/www.conf

# Copy PHPeek PM config
COPY phpeek-pm.yaml /etc/phpeek-pm/phpeek-pm.yaml

# Run as PID 1
ENTRYPOINT ["/usr/local/bin/phpeek-pm"]
```

## Customization Workflow

### 1. Generate Base Configuration

```bash
./phpeek-pm scaffold laravel --output ./myapp
```

### 2. Review Generated Files

```bash
cd ./myapp
cat phpeek-pm.yaml  # Check process configuration
```

### 3. Customize for Your Needs

**Adjust worker counts:**
```yaml
# Change scale from 3 to 5
processes:
  queue-default:
    scale: 5  # Was: 3
```

**Modify health check endpoints:**
```yaml
# Update health check URL
processes:
  nginx:
    health_check:
      address: "http://127.0.0.1:80/api/health"  # Custom endpoint
```

**Change log levels:**
```yaml
# Increase verbosity for debugging
global:
  log_level: debug  # Was: info
```

**Add/remove processes:**
```yaml
# Add new queue worker
processes:
  queue-emails:
    enabled: true
    command: ["php", "artisan", "queue:work", "--queue=emails"]
    scale: 2
```

### 4. Validate Configuration

```bash
phpeek-pm check-config --config ./myapp/phpeek-pm.yaml --strict
```

### 5. Test with Dry Run

```bash
phpeek-pm --config ./myapp/phpeek-pm.yaml --dry-run
```

### 6. Deploy

```bash
# Docker Compose
cd ./myapp && docker-compose up -d

# Or standalone
phpeek-pm --config ./myapp/phpeek-pm.yaml
```

## Preset Comparison Matrix

### PHP Presets

| Feature | Minimal | Generic | Symfony | Laravel | Production |
|---------|---------|---------|---------|---------|------------|
| PHP-FPM | - | - | ✅ | ✅ | ✅ |
| Nginx | - | ✅ | ✅ | ✅ | ✅ |
| Queue Workers | - | - | ✅ | ✅ | ✅ |
| Horizon | - | - | - | ✅ | ✅ |
| Scheduler | - | - | - | ✅ | ✅ |
| Health Checks | - | ✅ | ✅ | ✅ | ✅ |
| Metrics | - | ✅ | ✅ | ✅ | ✅ |
| API | - | ✅ | ✅ | ✅ | ✅ |
| Tracing | - | - | - | - | ✅ |

### CMS Presets

| Feature | WordPress | Magento 2 | Drupal |
|---------|-----------|-----------|--------|
| PHP-FPM | ✅ | ✅ | ✅ |
| Nginx | ✅ | ✅ | ✅ |
| Queue Workers | - | ✅ (2) | - |
| Scheduled Cron | ✅ (WP-CLI) | ✅ (bin/magento) | ✅ (Drush) |
| Indexer | - | ✅ | - |
| Health Checks | ✅ | ✅ | ✅ |
| Metrics | ✅ | ✅ | ✅ |
| API | ✅ | ✅ | ✅ |

### Node.js Presets

| Feature | Next.js | Nuxt 3 | Node.js |
|---------|---------|--------|---------|
| Node.js Server | ✅ (standalone) | ✅ (Nitro) | ✅ |
| Nginx (reverse proxy) | ✅ | ✅ | ✅ |
| Scale Instances | ✅ (2) | ✅ (2) | ✅ (2) |
| Memory Protection | ✅ (512MB) | ✅ (512MB) | ✅ (512MB) |
| Background Workers | - | - | ✅ |
| Health Checks | ✅ | ✅ | ✅ |
| Metrics | ✅ | ✅ | ✅ |
| API | ✅ | ✅ | ✅ |
| Port Base | ✅ (3000) | ✅ (3000) | ✅ (3000) |

## Examples

### Example 1: Quick Laravel Development Setup

```bash
# Generate Laravel config for local development
./phpeek-pm scaffold laravel \
  --output ./docker \
  --app-name my-laravel-app \
  --queue-workers 2

# Result: phpeek-pm.yaml with 2 queue workers, API + Metrics enabled
```

### Example 2: Production Deployment with Observability

```bash
# Generate production config with full observability stack
./phpeek-pm scaffold production \
  --output ./production \
  --app-name my-app-prod \
  --docker-compose

# Result:
# - phpeek-pm.yaml (with tracing, metrics, API)
# - docker-compose.yml (with Prometheus + Grafana)
```

### Example 3: Interactive Configuration

```bash
./phpeek-pm scaffold --interactive

# Sample session:
# Select a preset: 1 (Laravel)
# Application name [my-app]: demo-app
# Log level [info]: warn
# Number of queue workers [3]: 5
# Queue connection [redis]: redis
# Enable Prometheus metrics? [y]: y
# Enable Management API? [y]: y
# Enable distributed tracing? [n]: n
# Generate docker-compose.yml? [n]: y
# Generate Dockerfile? [n]: n
#
# ✅ Generated: phpeek-pm.yaml, docker-compose.yml
```

### Example 4: Minimal Customization Starting Point

```bash
# Start with minimal template for full customization
./phpeek-pm scaffold minimal --output ./custom

# Edit phpeek-pm.yaml manually to add your processes
vim ./custom/phpeek-pm.yaml
```

### Example 5: Symfony with Messenger

```bash
# Generate Symfony config with custom worker count
./phpeek-pm scaffold symfony \
  --output ./symfony-app \
  --app-name symfony-api \
  --queue-workers 4 \
  --dockerfile

# Result: Symfony config + Dockerfile
```

### Example 6: WordPress with Proper Cron

```bash
# Generate WordPress config with WP-CLI cron
./phpeek-pm scaffold wordpress \
  --output ./wordpress \
  --app-name my-blog \
  --dockerfile

# Result: phpeek-pm.yaml with WP-CLI cron (runs every minute)
# Don't forget: define('DISABLE_WP_CRON', true); in wp-config.php
```

### Example 7: Magento 2 E-commerce

```bash
# Generate Magento 2 config with full stack
./phpeek-pm scaffold magento \
  --output ./magento \
  --app-name my-store \
  --queue-workers 4 \
  --dockerfile \
  --docker-compose

# Result: Full Magento stack with queue consumers, cron, and indexer
```

### Example 8: Drupal CMS

```bash
# Generate Drupal config with Drush cron
./phpeek-pm scaffold drupal \
  --output ./drupal \
  --app-name my-cms \
  --dockerfile

# Result: Drupal config with Drush cron (runs every 5 minutes)
```

## Template Architecture

### Config Struct

**Located in:** `internal/scaffold/config.go`

```go
type Config struct {
    Preset          string  // laravel, symfony, generic, minimal, production
    EnableNginx     bool    // Include Nginx process
    EnableHorizon   bool    // Include Laravel Horizon
    EnableMetrics   bool    // Enable Prometheus
    EnableAPI       bool    // Enable Management API
    EnableTracing   bool    // Enable distributed tracing
    QueueWorkers    int     // Number of queue worker instances
    LogLevel        string  // debug, info, warn, error
    AppName         string  // Application name
}
```

### Templates

**Located in:** `internal/scaffold/templates.go`

**1. ConfigTemplate**
- Conditional YAML generation based on feature flags
- Process definitions with dependencies
- Health checks configuration
- Observability settings

**2. DockerComposeTemplate**
- Full stack with MySQL, Redis
- Prometheus + Grafana (production preset)
- Network and volume configuration
- Port mappings

**3. DockerfileTemplate**
- Multi-stage PHP 8.2 build
- PHP extensions installation
- Composer dependencies
- PHPeek PM binary integration

### Generator

**Located in:** `internal/scaffold/generator.go`

**Responsibilities:**
- Orchestrates file generation from templates
- Applies preset defaults
- Supports customization via setter methods
- Handles file writing with error checking

## Best Practices

### 1. Start with Preset Closest to Your Needs

```bash
# Don't start from scratch - choose similar preset
./phpeek-pm scaffold laravel  # For Laravel apps
./phpeek-pm scaffold symfony   # For Symfony apps
```

### 2. Validate Early

```bash
# Always validate after generation
./phpeek-pm check-config --config phpeek-pm.yaml --strict
```

### 3. Test with Dry Run

```bash
# Verify configuration before deployment
./phpeek-pm --config phpeek-pm.yaml --dry-run
```

### 4. Version Control

```bash
# Commit generated configs to Git for reproducibility
git add phpeek-pm.yaml docker-compose.yml
git commit -m "Add PHPeek PM configuration"
```

### 5. Use Environment Variables for Secrets

```yaml
# Don't hardcode secrets
global:
  api_auth: "${PHPEEK_PM_API_TOKEN}"  # Load from env

processes:
  app:
    env:
      DB_PASSWORD: "${DATABASE_PASSWORD}"  # Never hardcode
```

### 6. Customize Health Checks

```yaml
# Adjust periods/timeouts based on app characteristics
processes:
  nginx:
    health_check:
      interval: 10      # Check every 10s
      timeout: 5        # Fail after 5s
      retries: 3        # Retry 3 times before unhealthy
      success_threshold: 2  # 2 successes to mark healthy
```

### 7. Scale Appropriately

```yaml
# Start conservative with workers, scale based on metrics
processes:
  queue-default:
    scale: 3  # Start with 3, scale up as needed
```

### 8. Enable Observability

```yaml
# Always enable for staging/production
global:
  metrics_enabled: true
  api_enabled: true
  tracing_enabled: true  # Production preset
```

## Troubleshooting

### Scaffold Command Not Found

**Error:** `scaffold: command not found`

**Solution:**
```bash
# Ensure using correct PHPeek PM binary
./phpeek-pm scaffold laravel  # Local binary
phpeek-pm scaffold laravel     # Installed globally

# Check version
./phpeek-pm --version
```

### Permission Denied on Output Directory

**Error:** `mkdir: permission denied`

**Solution:**
```bash
# Ensure output directory is writable
mkdir -p ./output
chmod 755 ./output

# Or use different output location
./phpeek-pm scaffold laravel --output ~/projects/myapp
```

### Generated Config Has Errors

**Error:** Validation fails after generation

**Solution:**
```bash
# Check what's wrong
./phpeek-pm check-config --config phpeek-pm.yaml

# Common fixes:
# 1. Circular dependencies - remove or reorder depends_on
# 2. Invalid port numbers - use non-privileged ports (>1024)
# 3. Missing commands - ensure binaries exist in PATH
```

### Docker Compose Fails to Start

**Error:** `ERROR: Version in "./docker-compose.yml" is unsupported`

**Solution:**
```bash
# Check Docker Compose version
docker-compose --version

# Update docker-compose.yml version if needed
version: '3.8'  # Use compatible version
```

## Next Steps

- [Configuration Validation](../configuration/validation) - Validate generated configs
- [Quick Start](../getting-started/quickstart) - Deploy generated configuration
- [Configuration Overview](../configuration/overview) - Customize further
- [Examples](../examples/) - Real-world examples

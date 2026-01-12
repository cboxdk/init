package scaffold

import (
	"fmt"
	"strings"
	"text/template"
)

// Preset represents a configuration preset type
type Preset string

const (
	PresetLaravel   Preset = "laravel"
	PresetSymfony   Preset = "symfony"
	PresetPHP       Preset = "php"
	PresetWordPress Preset = "wordpress"
	PresetMagento   Preset = "magento"
	PresetDrupal    Preset = "drupal"
	PresetNextJS    Preset = "nextjs"
	PresetNuxt      Preset = "nuxt"
	PresetNodeJS    Preset = "nodejs"
)

// Config holds template configuration data
type Config struct {
	// Framework and app settings
	Preset    Preset
	AppName   string
	Framework string

	// Feature flags
	EnableNginx     bool
	EnableHorizon   bool
	EnableQueue     bool
	EnableScheduler bool
	EnableMetrics   bool
	EnableAPI       bool
	EnableTracing   bool

	// Process configuration
	PHPFPMWorkers   int
	QueueWorkers    int
	QueueConnection string

	// Node.js specific
	NodeInstances  int    // Number of Node.js app instances
	PortBase       int    // Base port for Node.js instances
	MaxMemoryMB    int    // Memory limit for Node.js processes
	NodeCommand    string // Node.js start command
	EnableWorkers  bool   // Enable background workers (for Node.js)

	// Ports
	APIPort     int
	MetricsPort int

	// Paths
	WorkDir  string
	LogLevel string
}

// DefaultConfig returns default configuration for a preset
func DefaultConfig(preset Preset) *Config {
	cfg := &Config{
		Preset:          preset,
		AppName:         "my-app",
		WorkDir:         "/var/www/html",
		LogLevel:        "info",
		EnableMetrics:   true,
		EnableAPI:       true,
		APIPort:         9180,
		MetricsPort:     9090,
		PHPFPMWorkers:   5,
		QueueWorkers:    3,
		QueueConnection: "redis",
		// Node.js defaults
		NodeInstances: 1,
		PortBase:      3000,
		MaxMemoryMB:   512,
	}

	switch preset {
	case PresetLaravel:
		cfg.Framework = "laravel"
		cfg.EnableNginx = true
		cfg.EnableHorizon = true
		cfg.EnableQueue = true
		cfg.EnableScheduler = true
	case PresetSymfony:
		cfg.Framework = "symfony"
		cfg.EnableNginx = true
		cfg.EnableQueue = true
	case PresetPHP:
		cfg.Framework = "php"
		cfg.EnableNginx = true
	case PresetNextJS:
		cfg.Framework = "nextjs"
		cfg.WorkDir = "/app"
		cfg.EnableNginx = true
		cfg.NodeCommand = "node .next/standalone/server.js"
		cfg.NodeInstances = 2
		cfg.PortBase = 3000
		cfg.MaxMemoryMB = 512
	case PresetNuxt:
		cfg.Framework = "nuxt"
		cfg.WorkDir = "/app"
		cfg.EnableNginx = true
		cfg.NodeCommand = "node .output/server/index.mjs"
		cfg.NodeInstances = 2
		cfg.PortBase = 3000
		cfg.MaxMemoryMB = 512
	case PresetNodeJS:
		cfg.Framework = "nodejs"
		cfg.WorkDir = "/app"
		cfg.EnableNginx = true
		cfg.NodeCommand = "node dist/server.js"
		cfg.NodeInstances = 2
		cfg.PortBase = 3000
		cfg.MaxMemoryMB = 512
		cfg.EnableWorkers = true
		cfg.QueueWorkers = 2
	case PresetWordPress:
		cfg.Framework = "wordpress"
		cfg.EnableNginx = true
		cfg.EnableScheduler = true // WP-Cron replacement
	case PresetMagento:
		cfg.Framework = "magento"
		cfg.EnableNginx = true
		cfg.EnableQueue = true
		cfg.EnableScheduler = true
		cfg.QueueWorkers = 2
	case PresetDrupal:
		cfg.Framework = "drupal"
		cfg.EnableNginx = true
		cfg.EnableScheduler = true // Drush cron
	}

	return cfg
}

// ConfigTemplate is the main configuration template
const ConfigTemplate = `version: "1.0"

global:
  shutdown_timeout: 30
  log_level: {{ .LogLevel }}
  log_format: json
  {{- if .EnableAPI }}

  # Management API
  api_enabled: true
  api_port: {{ .APIPort }}
  {{- end }}
  {{- if .EnableMetrics }}

  # Prometheus Metrics
  metrics_enabled: true
  metrics_port: {{ .MetricsPort }}
  metrics_path: /metrics
  {{- end }}
  {{- if .EnableTracing }}

  # Distributed Tracing
  tracing_enabled: true
  tracing_exporter: otlp-grpc
  tracing_endpoint: localhost:4317
  tracing_sample_rate: 0.1
  tracing_service_name: {{ .AppName }}
  {{- end }}

  # Restart configuration
  restart_backoff_initial: 1s
  restart_backoff_max: 60s
  max_restart_attempts: 5

processes:
  {{- if eq .Framework "laravel" }}
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    type: longrun
    restart: always
    scale: 1
    stdout: true
    stderr: true
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      period: 30
      timeout: 5
      failure_threshold: 3
  {{- else if eq .Framework "symfony" }}
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    type: longrun
    restart: always
    scale: 1
    stdout: true
    stderr: true
  {{- else if eq .Framework "nextjs" }}
  # Next.js production server (standalone mode)
  nextjs:
    enabled: true
    command: ["node", ".next/standalone/server.js"]
    type: longrun
    restart: always
    scale: {{ .NodeInstances }}
    working_dir: {{ .WorkDir }}
    port_base: {{ .PortBase }}
    max_memory_mb: {{ .MaxMemoryMB }}
    env:
      NODE_ENV: production
      HOSTNAME: "0.0.0.0"
    stdout: true
    stderr: true
    health_check:
      type: http
      url: "http://127.0.0.1:{{ .PortBase }}/api/health"
      initial_delay: 5
      period: 10
      timeout: 3
      failure_threshold: 3
    shutdown:
      signal: SIGTERM
      timeout: 30
  {{- else if eq .Framework "nuxt" }}
  # Nuxt 3 Nitro server
  nuxt:
    enabled: true
    command: ["node", ".output/server/index.mjs"]
    type: longrun
    restart: always
    scale: {{ .NodeInstances }}
    working_dir: {{ .WorkDir }}
    port_base: {{ .PortBase }}
    max_memory_mb: {{ .MaxMemoryMB }}
    env:
      NODE_ENV: production
      NITRO_HOST: "0.0.0.0"
    stdout: true
    stderr: true
    health_check:
      type: http
      url: "http://127.0.0.1:{{ .PortBase }}/api/health"
      initial_delay: 5
      period: 10
      timeout: 3
      failure_threshold: 3
    shutdown:
      signal: SIGTERM
      timeout: 30
  {{- else if eq .Framework "nodejs" }}
  # Node.js application server
  app:
    enabled: true
    command: ["node", "dist/server.js"]
    type: longrun
    restart: always
    scale: {{ .NodeInstances }}
    working_dir: {{ .WorkDir }}
    port_base: {{ .PortBase }}
    max_memory_mb: {{ .MaxMemoryMB }}
    env:
      NODE_ENV: production
    stdout: true
    stderr: true
    health_check:
      type: http
      url: "http://127.0.0.1:{{ .PortBase }}/health"
      initial_delay: 5
      period: 10
      timeout: 3
      failure_threshold: 3
    shutdown:
      signal: SIGTERM
      timeout: 30
  {{- else if eq .Framework "wordpress" }}
  # WordPress with PHP-FPM
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    type: longrun
    restart: always
    scale: 1
    stdout: true
    stderr: true
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      period: 30
      timeout: 5
      failure_threshold: 3
  {{- else if eq .Framework "magento" }}
  # Magento 2 with PHP-FPM
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    type: longrun
    restart: always
    scale: 1
    stdout: true
    stderr: true
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      period: 30
      timeout: 5
      failure_threshold: 3
  {{- else if eq .Framework "drupal" }}
  # Drupal with PHP-FPM
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    type: longrun
    restart: always
    scale: 1
    stdout: true
    stderr: true
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      period: 30
      timeout: 5
      failure_threshold: 3
  {{- else if eq .Framework "php" }}
  # Vanilla PHP with PHP-FPM
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    type: longrun
    restart: always
    scale: 1
    stdout: true
    stderr: true
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      period: 30
      timeout: 5
      failure_threshold: 3
  {{- end }}
  {{- if and .EnableNginx (or (eq .Framework "laravel") (eq .Framework "symfony") (eq .Framework "php") (eq .Framework "wordpress") (eq .Framework "magento") (eq .Framework "drupal")) }}

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    type: longrun
    restart: always
    scale: 1
    {{- if eq .Framework "laravel" }}
    depends_on:
      - php-fpm
    {{- end }}
    stdout: true
    stderr: true
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      period: 30
      timeout: 5
      failure_threshold: 3
      expected_status: 200
  {{- else if and .EnableNginx (or (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs")) }}

  # Nginx reverse proxy for Node.js
  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    type: longrun
    restart: always
    scale: 1
    {{- if eq .Framework "nextjs" }}
    depends_on:
      - nextjs
    {{- else if eq .Framework "nuxt" }}
    depends_on:
      - nuxt
    {{- else }}
    depends_on:
      - app
    {{- end }}
    stdout: true
    stderr: true
    health_check:
      type: http
      url: "http://127.0.0.1:80/health"
      period: 10
      timeout: 3
      failure_threshold: 3
      expected_status: 200
  {{- end }}
  {{- if .EnableHorizon }}

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    type: longrun
    restart: always
    scale: 1
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true
    shutdown:
      pre_stop_hook:
        command: ["php", "artisan", "horizon:terminate"]
        timeout: 60
  {{- end }}
  {{- if and .EnableQueue (or (eq .Framework "laravel") (eq .Framework "symfony")) }}

  queue-default:
    enabled: true
    command: ["php", "artisan", "queue:work", "{{ .QueueConnection }}", "--queue=default", "--tries=3"]
    type: longrun
    restart: always
    scale: {{ .QueueWorkers }}
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true
  {{- end }}
  {{- if and .EnableQueue (eq .Framework "magento") }}

  # Magento queue consumers
  queue-consumer:
    enabled: true
    command: ["php", "bin/magento", "queue:consumers:start", "--max-messages=10000"]
    type: longrun
    restart: always
    scale: {{ .QueueWorkers }}
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true
  {{- end }}
  {{- if and .EnableWorkers (eq .Framework "nodejs") }}

  # Background job workers
  worker:
    enabled: true
    command: ["node", "dist/worker.js"]
    type: longrun
    restart: always
    scale: {{ .QueueWorkers }}
    working_dir: {{ .WorkDir }}
    max_memory_mb: 256
    depends_on:
      - app
    env:
      NODE_ENV: production
    stdout: true
    stderr: true
  {{- end }}
  {{- if and .EnableScheduler (or (eq .Framework "laravel") (eq .Framework "symfony")) }}

  scheduler:
    enabled: true
    command: ["php", "artisan", "schedule:work"]
    type: longrun
    restart: always
    scale: 1
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true
  {{- end }}
  {{- if and .EnableScheduler (eq .Framework "wordpress") }}

  # WordPress WP-Cron replacement (more reliable than wp-cron.php)
  wp-cron:
    enabled: true
    command: ["wp", "cron", "event", "run", "--due-now"]
    type: oneshot
    restart: never
    schedule: "*/1 * * * *"
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true
  {{- end }}
  {{- if and .EnableScheduler (eq .Framework "magento") }}

  # Magento cron jobs
  cron:
    enabled: true
    command: ["php", "bin/magento", "cron:run"]
    type: oneshot
    restart: never
    schedule: "* * * * *"
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true

  # Magento index management
  indexer:
    enabled: true
    command: ["php", "bin/magento", "indexer:reindex"]
    type: oneshot
    restart: never
    schedule: "*/15 * * * *"
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true
  {{- end }}
  {{- if and .EnableScheduler (eq .Framework "drupal") }}

  # Drupal cron via Drush
  drush-cron:
    enabled: true
    command: ["drush", "cron"]
    type: oneshot
    restart: never
    schedule: "*/5 * * * *"
    working_dir: {{ .WorkDir }}
    stdout: true
    stderr: true
  {{- end }}
`

// DockerComposeTemplate generates a docker-compose.yml file
const DockerComposeTemplate = `version: '3.8'

services:
  app:
    image: {{ .AppName }}:latest
    container_name: {{ .AppName }}
    build:
      context: .
      dockerfile: Dockerfile
    restart: unless-stopped
    ports:
      {{- if .EnableNginx }}
      - "80:80"
      {{- end }}
      {{- if .EnableAPI }}
      - "{{ .APIPort }}:{{ .APIPort }}"
      {{- end }}
      {{- if .EnableMetrics }}
      - "{{ .MetricsPort }}:{{ .MetricsPort }}"
      {{- end }}
    volumes:
      - ./phpeek-pm.yaml:/etc/phpeek-pm/config.yaml:ro
      {{- if .EnableNginx }}
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      {{- end }}
    environment:
      {{- if or (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs") }}
      - NODE_ENV=production
      {{- if eq .Framework "nextjs" }}
      - HOSTNAME=0.0.0.0
      {{- else if eq .Framework "nuxt" }}
      - NITRO_HOST=0.0.0.0
      {{- end }}
      {{- else }}
      - APP_ENV=production
      - APP_DEBUG=false
      {{- end }}
      {{- if eq .Framework "laravel" }}
      - DB_CONNECTION=mysql
      - DB_HOST=db
      - DB_PORT=3306
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      {{- end }}
      {{- if or (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs") }}
      - DATABASE_URL=postgresql://postgres:secret@db:5432/{{ .AppName }}
      - REDIS_URL=redis://redis:6379
      {{- end }}
    depends_on:
      {{- if eq .Framework "laravel" }}
      - db
      - redis
      {{- end }}
      {{- if or (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs") }}
      - db
      - redis
      {{- end }}
    networks:
      - app-network
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:80/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
{{- if eq .Framework "laravel" }}

  db:
    image: mysql:8.0
    container_name: {{ .AppName }}-db
    restart: unless-stopped
    environment:
      - MYSQL_ROOT_PASSWORD=secret
      - MYSQL_DATABASE={{ .AppName }}
      - MYSQL_USER={{ .AppName }}
      - MYSQL_PASSWORD=secret
    volumes:
      - db-data:/var/lib/mysql
    networks:
      - app-network

  redis:
    image: redis:7-alpine
    container_name: {{ .AppName }}-redis
    restart: unless-stopped
    volumes:
      - redis-data:/data
    networks:
      - app-network
{{- end }}
{{- if or (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs") }}

  db:
    image: postgres:16-alpine
    container_name: {{ .AppName }}-db
    restart: unless-stopped
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=secret
      - POSTGRES_DB={{ .AppName }}
    volumes:
      - db-data:/var/lib/postgresql/data
    networks:
      - app-network
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    container_name: {{ .AppName }}-redis
    restart: unless-stopped
    volumes:
      - redis-data:/data
    networks:
      - app-network
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
{{- end }}
{{- if .EnableMetrics }}

  prometheus:
    image: prom/prometheus:latest
    container_name: {{ .AppName }}-prometheus
    restart: unless-stopped
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    networks:
      - app-network

  grafana:
    image: grafana/grafana:latest
    container_name: {{ .AppName }}-grafana
    restart: unless-stopped
    ports:
      - "3001:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana-data:/var/lib/grafana
    networks:
      - app-network
{{- end }}

networks:
  app-network:
    driver: bridge

volumes:
{{- if or (eq .Framework "laravel") (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs") }}
  db-data:
  redis-data:
{{- end }}
{{- if .EnableMetrics }}
  prometheus-data:
  grafana-data:
{{- end }}
`

// DockerfileTemplate generates a Dockerfile (PHP or Node.js based on framework)
// PHP frameworks use gophpeek/php-fpm-nginx base images with PHPeek PM pre-installed
// Node.js frameworks use multi-stage builds with PHPeek PM binary
const DockerfileTemplate = `{{- if or (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs") -}}
# =============================================================================
# Node.js Application Dockerfile with PHPeek PM
# Multi-stage build optimized for {{ .Framework | title }}
# =============================================================================

# Stage 1: Dependencies
FROM node:22-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm ci

# Stage 2: Builder
FROM node:22-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
{{- if eq .Framework "nextjs" }}
# Next.js: Enable standalone output in next.config.js
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build
{{- else if eq .Framework "nuxt" }}
# Nuxt 3: Build with Nitro preset
RUN npm run build
{{- else }}
# Generic Node.js build
RUN npm run build 2>/dev/null || true
{{- end }}

# Stage 3: Production
FROM node:22-alpine AS runner
LABEL maintainer="PHPeek <https://github.com/gophpeek>"

# Install nginx and required tools
RUN apk add --no-cache nginx curl tini && \
    mkdir -p /var/log/nginx /var/lib/nginx/tmp && \
    chown -R node:node /var/log/nginx /var/lib/nginx

WORKDIR {{ .WorkDir }}

# Copy PHPeek PM binary (download from releases or copy from build context)
# Option 1: Copy from build context
# COPY phpeek-pm /usr/local/bin/phpeek-pm
# Option 2: Download from releases (uncomment the appropriate architecture)
ARG PHPEEK_PM_VERSION=latest
ARG TARGETARCH
RUN curl -fsSL "https://github.com/gophpeek/phpeek-pm/releases/${PHPEEK_PM_VERSION}/download/phpeek-pm-linux-${TARGETARCH}" \
    -o /usr/local/bin/phpeek-pm && \
    chmod +x /usr/local/bin/phpeek-pm

# Copy built application
{{- if eq .Framework "nextjs" }}
COPY --from=builder --chown=node:node /app/.next/standalone ./
COPY --from=builder --chown=node:node /app/.next/static ./.next/static
COPY --from=builder --chown=node:node /app/public ./public
{{- else if eq .Framework "nuxt" }}
COPY --from=builder --chown=node:node /app/.output ./.output
{{- else }}
COPY --from=builder --chown=node:node /app/dist ./dist
COPY --from=builder --chown=node:node /app/node_modules ./node_modules
COPY --from=builder --chown=node:node /app/package*.json ./
{{- end }}

# Copy configuration files
COPY --chown=node:node phpeek-pm.yaml /etc/phpeek-pm/config.yaml
{{- if .EnableNginx }}
COPY --chown=node:node nginx.conf /etc/nginx/nginx.conf
{{- end }}

# Environment
ENV NODE_ENV=production
{{- if eq .Framework "nextjs" }}
ENV HOSTNAME=0.0.0.0
{{- else if eq .Framework "nuxt" }}
ENV NITRO_HOST=0.0.0.0
{{- end }}

# Expose ports
{{- if .EnableNginx }}
EXPOSE 80
{{- end }}
{{- if .EnableAPI }}
EXPOSE {{ .APIPort }}
{{- end }}
{{- if .EnableMetrics }}
EXPOSE {{ .MetricsPort }}
{{- end }}

# Use tini as init system, PHPeek PM as process manager
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/local/bin/phpeek-pm", "serve", "--config", "/etc/phpeek-pm/config.yaml"]
{{- else -}}
# =============================================================================
# PHP Application Dockerfile using gophpeek/php-fpm-nginx base image
# PHPeek PM is pre-installed and configured in the base image
# =============================================================================

# Available tiers: slim (120MB), standard (250MB), full (700MB)
# Available versions: 8.2, 8.3, 8.4
FROM gophpeek/php-fpm-nginx:8.3-bookworm

LABEL maintainer="PHPeek <https://github.com/gophpeek>"

# Set working directory
WORKDIR {{ .WorkDir }}

# Copy composer files first for better caching
COPY composer.json composer.lock* ./

# Install PHP dependencies
RUN composer install --no-dev --optimize-autoloader --no-scripts --no-interaction

# Copy application code
COPY . .

{{- if eq .Framework "laravel" }}
# Laravel-specific optimizations
RUN php artisan config:cache && \
    php artisan route:cache && \
    php artisan view:cache && \
    chown -R www-data:www-data storage bootstrap/cache
{{- else if eq .Framework "symfony" }}
# Symfony-specific optimizations
RUN composer dump-autoload --optimize && \
    php bin/console cache:warmup --env=prod
{{- end }}

# Copy PHPeek PM configuration (overrides default)
COPY phpeek-pm.yaml /etc/phpeek-pm/config.yaml

# The base image already exposes ports 80, 443, 9180 (API), 9090 (metrics)
# and uses PHPeek PM as PID 1 with auto-detection for Laravel/Symfony/WordPress
{{- end }}
`

// NginxConfigTemplate generates nginx.conf for Node.js load balancing
const NginxConfigTemplate = `{{- if or (eq .Framework "nextjs") (eq .Framework "nuxt") (eq .Framework "nodejs") -}}
# Nginx configuration for {{ .AppName }} ({{ .Framework }})
# Load balancing across {{ .NodeInstances }} Node.js instances

worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
    use epoll;
    multi_accept on;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for" '
                    'rt=$request_time uct="$upstream_connect_time" '
                    'uht="$upstream_header_time" urt="$upstream_response_time"';

    access_log /var/log/nginx/access.log main;

    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_types text/plain text/css text/xml application/json application/javascript
               application/xml application/xml+rss text/javascript application/x-javascript;

    # Upstream pool for Node.js instances
    # PHPeek PM assigns PORT = port_base + instance_index
    upstream nodejs_backend {
        least_conn;  # Load balancing method
        keepalive 32;
        {{- range $i := iterate .NodeInstances }}
        server 127.0.0.1:{{ add $.PortBase $i }} weight=1 max_fails=3 fail_timeout=30s;
        {{- end }}
    }

    server {
        listen 80;
        server_name _;

        # Security headers
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-XSS-Protection "1; mode=block" always;

        # Health check endpoint (handled by nginx directly)
        location /health {
            access_log off;
            return 200 'healthy';
            add_header Content-Type text/plain;
        }

        # Nginx status for monitoring
        location /nginx_status {
            stub_status on;
            access_log off;
            allow 127.0.0.1;
            deny all;
        }
        {{- if eq .Framework "nextjs" }}

        # Next.js static files
        location /_next/static {
            alias {{ .WorkDir }}/.next/static;
            expires 1y;
            access_log off;
            add_header Cache-Control "public, immutable";
        }

        # Next.js public files
        location /public {
            alias {{ .WorkDir }}/public;
            expires 1d;
            access_log off;
        }
        {{- else if eq .Framework "nuxt" }}

        # Nuxt static files
        location /_nuxt {
            alias {{ .WorkDir }}/.output/public/_nuxt;
            expires 1y;
            access_log off;
            add_header Cache-Control "public, immutable";
        }

        # Nuxt public files
        location /public {
            alias {{ .WorkDir }}/.output/public;
            expires 1d;
            access_log off;
        }
        {{- end }}

        # Proxy to Node.js upstream
        location / {
            proxy_pass http://nodejs_backend;
            proxy_http_version 1.1;

            # WebSocket support
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection 'upgrade';

            # Headers
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header X-Request-ID $request_id;

            # Timeouts
            proxy_connect_timeout 60s;
            proxy_send_timeout 60s;
            proxy_read_timeout 60s;

            # Buffering
            proxy_buffering on;
            proxy_buffer_size 4k;
            proxy_buffers 8 4k;
            proxy_busy_buffers_size 8k;

            # Cache bypass for dynamic content
            proxy_cache_bypass $http_upgrade;
        }
    }
}
{{- else -}}
# Nginx configuration for {{ .AppName }} (PHP-FPM)

worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent"';

    access_log /var/log/nginx/access.log main;

    sendfile on;
    keepalive_timeout 65;
    gzip on;

    server {
        listen 80;
        server_name _;
        root {{ .WorkDir }}/public;
        index index.php index.html;

        # Health check
        location /health {
            access_log off;
            return 200 'healthy';
            add_header Content-Type text/plain;
        }

        location / {
            try_files $uri $uri/ /index.php?$query_string;
        }

        location ~ \.php$ {
            fastcgi_pass 127.0.0.1:9000;
            fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
            include fastcgi_params;
        }

        location ~ /\.(?!well-known).* {
            deny all;
        }
    }
}
{{- end }}
`

// GenerateConfig generates configuration file from template
func GenerateConfig(cfg *Config) (string, error) {
	tmpl, err := template.New("config").Parse(ConfigTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse config template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("failed to execute config template: %w", err)
	}

	return buf.String(), nil
}

// GenerateDockerCompose generates docker-compose.yml from template
func GenerateDockerCompose(cfg *Config) (string, error) {
	tmpl, err := template.New("docker-compose").Parse(DockerComposeTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse docker-compose template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("failed to execute docker-compose template: %w", err)
	}

	return buf.String(), nil
}

// GenerateDockerfile generates Dockerfile from template
func GenerateDockerfile(cfg *Config) (string, error) {
	tmpl, err := template.New("dockerfile").Funcs(templateFuncs).Parse(DockerfileTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse dockerfile template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("failed to execute dockerfile template: %w", err)
	}

	return buf.String(), nil
}

// templateFuncs provides custom functions for templates
var templateFuncs = template.FuncMap{
	"iterate": func(count int) []int {
		result := make([]int, count)
		for i := range result {
			result[i] = i
		}
		return result
	},
	"add": func(a, b int) int {
		return a + b
	},
	"title": strings.Title, //nolint:staticcheck // strings.Title is fine for framework names
}

// GenerateNginxConfig generates nginx.conf from template
func GenerateNginxConfig(cfg *Config) (string, error) {
	tmpl, err := template.New("nginx").Funcs(templateFuncs).Parse(NginxConfigTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse nginx config template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("failed to execute nginx config template: %w", err)
	}

	return buf.String(), nil
}

// ValidPresets returns list of valid preset names
func ValidPresets() []string {
	return []string{
		string(PresetLaravel),
		string(PresetSymfony),
		string(PresetPHP),
		string(PresetWordPress),
		string(PresetMagento),
		string(PresetDrupal),
		string(PresetNextJS),
		string(PresetNuxt),
		string(PresetNodeJS),
	}
}

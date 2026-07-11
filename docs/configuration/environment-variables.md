---
title: "Environment Variables"
description: "Override configuration with environment variables for flexible deployment across environments"
weight: 17
---

# Environment Variables

Override any YAML configuration using environment variables for flexible, environment-specific deployments.

## Overview

Environment variables enable:
- ✅ **Configuration without file changes:** Adjust settings per environment
- ✅ **Secret management:** Keep sensitive values out of version control
- ✅ **Container orchestration:** Easy Kubernetes/Docker configuration
- ✅ **CI/CD integration:** Dynamic configuration in pipelines
- ✅ **12-Factor compliance:** Externalized configuration

## Priority Order

Configuration is loaded in this order (later overrides earlier):

1. **Default values** - Built-in defaults
2. **YAML configuration file** - `cbox-init.yaml`
3. **Environment variables** - Runtime overrides

```bash
# YAML has log_level: info
# ENV overrides to debug
CBOX_INIT_GLOBAL_LOG_LEVEL=debug ./cbox-init
```

## Naming Convention

### Global Settings

**Pattern:** `CBOX_INIT_GLOBAL_<SETTING_NAME>`

```bash
CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT=60
CBOX_INIT_GLOBAL_LOG_LEVEL=debug
CBOX_INIT_GLOBAL_LOG_FORMAT=json
CBOX_INIT_GLOBAL_METRICS_ENABLED=true
CBOX_INIT_GLOBAL_METRICS_PORT=9090
CBOX_INIT_GLOBAL_API_ENABLED=true
CBOX_INIT_GLOBAL_API_PORT=8080
```

### Process-Specific Settings

**Pattern:** `CBOX_INIT_PROCESS_<PROCESS_NAME>_<SETTING_NAME>`

```bash
CBOX_INIT_PROCESS_NGINX_ENABLED=true
CBOX_INIT_PROCESS_NGINX_PRIORITY=20
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=5
CBOX_INIT_PROCESS_HORIZON_RESTART=always
```

**Important:** Process names are converted to uppercase and hyphens to underscores.

| Process Name | Environment Prefix |
|--------------|-------------------|
| `nginx` | `CBOX_INIT_PROCESS_NGINX_` |
| `queue-default` | `CBOX_INIT_PROCESS_QUEUE_DEFAULT_` |
| `php-fpm` | `CBOX_INIT_PROCESS_PHP_FPM_` |

### PHP-FPM Auto-Tuning

**Pattern:** `PHP_FPM_AUTOTUNE_PROFILE`

```bash
PHP_FPM_AUTOTUNE_PROFILE=medium
PHP_FPM_AUTOTUNE_PROFILE=heavy
```

See [PHP-FPM Auto-Tuning](php-fpm-autotune) for complete guide.

## Startup Performance Controls

These variables are useful for production images where framework directories and php-fpm/nginx configuration have already been prepared during image build.

| Variable | CLI flag | Description |
|----------|----------|-------------|
| `CBOX_INIT_SKIP_PERMISSION_SETUP=true` | `--skip-permission-setup` | Skip framework directory creation and recursive ownership fixes at startup. |
| `CBOX_INIT_SKIP_RUNTIME_VALIDATION=true` | `--skip-runtime-validation` | Skip `php-fpm -t` and `nginx -t` validation before process startup. |
| `CBOX_INIT_STARTUP_TIMING=true` | N/A | Log duration for startup phases such as permission setup, runtime validation, config load, and process start. |

Use the skip options only when the image build or deployment pipeline already validates the same assumptions:

```bash
docker run \
  -e CBOX_INIT_SKIP_PERMISSION_SETUP=true \
  -e CBOX_INIT_SKIP_RUNTIME_VALIDATION=true \
  -e CBOX_INIT_STARTUP_TIMING=true \
  myapp
```

## Permission / Ownership

These environment variables control which uid/gid cbox-init uses when chowning framework directories (Laravel `storage/`, Symfony `var/`, WordPress `wp-content/`).

**Note:** These are standalone variables — they do **not** follow the `CBOX_INIT_` prefix convention because they are a widely adopted container convention (used by linuxserver.io images, s6-overlay, etc.).

| Variable | Description | Default behaviour |
|----------|-------------|-------------------|
| `PUID` | User ID for framework directory ownership | Auto-detected from `/etc/passwd` |
| `PGID` | Group ID for framework directory ownership | Auto-detected from `/etc/passwd` |

### Resolution order

cbox-init resolves the app user in this order:

1. **`PUID` + `PGID` environment variables** — explicit operator override. Both must be set to valid non-negative integers; if either is missing or invalid, the override is skipped entirely.
2. **`/etc/passwd` lookup of `www-data`** — works on both Debian (uid 33) and Alpine (uid 82) without hardcoding either.
3. **Fallback to uid 82 / gid 82** (Alpine convention) — only used when the lookup also fails.

The resolved source is logged at startup so you can verify which path was taken:

```
INFO App user from PUID/PGID env uid=33 gid=33
# or
INFO App user from /etc/passwd lookup user=www-data uid=33 gid=33
```

### Examples

```bash
# Explicit override (e.g., match your host user for bind-mount permissions)
docker run -e PUID=1000 -e PGID=1000 myapp

# Kubernetes — set via env in the pod spec
env:
  - name: PUID
    value: "33"
  - name: PGID
    value: "33"
```

## Global Settings Reference

### Shutdown Configuration

```bash
# Shutdown timeout (seconds)
CBOX_INIT_GLOBAL_SHUTDOWN_TIMEOUT=60
```

### Logging Configuration

```bash
# Log format (json|text)
CBOX_INIT_GLOBAL_LOG_FORMAT=json

# Log level (debug|info|warn|error)
CBOX_INIT_GLOBAL_LOG_LEVEL=info

# Multiline logging
CBOX_INIT_GLOBAL_LOG_MULTILINE_ENABLED=true
CBOX_INIT_GLOBAL_LOG_MULTILINE_TIMEOUT=500

# Log redaction
CBOX_INIT_GLOBAL_LOG_REDACTION_ENABLED=true
```

### Metrics Configuration

```bash
# Enable Prometheus metrics
CBOX_INIT_GLOBAL_METRICS_ENABLED=true

# Metrics HTTP port
CBOX_INIT_GLOBAL_METRICS_PORT=9090

# Metrics URL path
CBOX_INIT_GLOBAL_METRICS_PATH=/metrics
```

### Restart Configuration

```bash
# Exponential backoff (Go duration strings)
CBOX_INIT_GLOBAL_RESTART_BACKOFF_INITIAL=5s
CBOX_INIT_GLOBAL_RESTART_BACKOFF_MAX=60s

# Maximum automatic restart attempts (0 = unlimited)
CBOX_INIT_GLOBAL_MAX_RESTART_ATTEMPTS=5
```

### API Configuration

```bash
# Enable Management API
CBOX_INIT_GLOBAL_API_ENABLED=true

# API HTTP port
CBOX_INIT_GLOBAL_API_PORT=8080

# API authentication token
CBOX_INIT_GLOBAL_API_AUTH=your-secure-token-here
```

## Process Settings Reference

### Basic Process Settings

```bash
# Enable/disable process
CBOX_INIT_PROCESS_<NAME>_ENABLED=true

# Command (JSON array)
CBOX_INIT_PROCESS_<NAME>_COMMAND='["php-fpm","-F","-R"]'

# Priority (startup order)
CBOX_INIT_PROCESS_<NAME>_PRIORITY=10

# Restart policy (always|on-failure|never)
CBOX_INIT_PROCESS_<NAME>_RESTART=always

# Scale (number of instances)
CBOX_INIT_PROCESS_<NAME>_SCALE=3

# Working directory
CBOX_INIT_PROCESS_<NAME>_WORKING_DIR=/var/www/html
```

### Process Environment Variables

```bash
# Set environment variable for process
CBOX_INIT_PROCESS_<NAME>_ENV_<VAR_NAME>=value

# Examples:
CBOX_INIT_PROCESS_QUEUE_ENV_QUEUE_CONNECTION=redis
CBOX_INIT_PROCESS_QUEUE_ENV_REDIS_HOST=localhost
CBOX_INIT_PROCESS_APP_ENV_DEBUG=true
```

## Complete Examples

### Docker Compose

```yaml
version: '3.8'

services:
  app:
    image: myapp:latest
    environment:
      # Global settings
      CBOX_INIT_GLOBAL_LOG_LEVEL: "info"
      CBOX_INIT_GLOBAL_METRICS_ENABLED: "true"
      CBOX_INIT_GLOBAL_API_ENABLED: "true"

      # PHP-FPM auto-tuning
      PHP_FPM_AUTOTUNE_PROFILE: "medium"

      # Process-specific
      CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE: "5"
      CBOX_INIT_PROCESS_HORIZON_ENABLED: "true"

    deploy:
      resources:
        limits:
          memory: 2G
          cpus: '2'
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: laravel-app
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: myapp:v1.2.3
        env:
          # Global config
          - name: CBOX_INIT_GLOBAL_LOG_FORMAT
            value: "json"

          - name: CBOX_INIT_GLOBAL_METRICS_ENABLED
            value: "true"

          # PHP-FPM auto-tuning from ConfigMap
          - name: PHP_FPM_AUTOTUNE_PROFILE
            valueFrom:
              configMapKeyRef:
                name: cbox-config
                key: php_fpm_profile

          # API token from Secret
          - name: CBOX_INIT_GLOBAL_API_AUTH
            valueFrom:
              secretKeyRef:
                name: cbox-secrets
                key: api-token

          # Process scaling
          - name: CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE
            value: "5"

        resources:
          limits:
            memory: "2Gi"
            cpu: "2"
```

### Dockerfile

```dockerfile
FROM php:8.3-fpm-alpine

# Copy application
COPY . /var/www/html

# Copy cbox-init
COPY --from=builder /app/cbox-init /usr/local/bin/cbox-init

# Default environment variables
ENV CBOX_INIT_GLOBAL_LOG_FORMAT=json \
    CBOX_INIT_GLOBAL_LOG_LEVEL=info \
    PHP_FPM_AUTOTUNE_PROFILE=medium

# Run as PID 1
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

### Shell Script

```bash
#!/bin/bash

# Production environment
export CBOX_INIT_GLOBAL_LOG_LEVEL=info
export CBOX_INIT_GLOBAL_METRICS_ENABLED=true
export CBOX_INIT_GLOBAL_API_ENABLED=true
export CBOX_INIT_GLOBAL_API_AUTH=$(cat /secrets/api-token)

# PHP-FPM configuration
export PHP_FPM_AUTOTUNE_PROFILE=heavy

# Process configuration
export CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=10
export CBOX_INIT_PROCESS_HORIZON_ENABLED=true

# Run cbox-init
exec /usr/local/bin/cbox-init
```

## Environment-Specific Patterns

### Development

```bash
# development.env
CBOX_INIT_GLOBAL_LOG_LEVEL=debug
CBOX_INIT_GLOBAL_LOG_FORMAT=text  # Human-readable
CBOX_INIT_GLOBAL_METRICS_ENABLED=false
PHP_FPM_AUTOTUNE_PROFILE=dev
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=1
```

```bash
# Run with development settings
set -a; source development.env; set +a
./cbox-init
```

### Staging

```bash
# staging.env
CBOX_INIT_GLOBAL_LOG_LEVEL=info
CBOX_INIT_GLOBAL_LOG_FORMAT=json
CBOX_INIT_GLOBAL_METRICS_ENABLED=true
PHP_FPM_AUTOTUNE_PROFILE=medium
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=3
```

### Production

```bash
# production.env
CBOX_INIT_GLOBAL_LOG_LEVEL=warn
CBOX_INIT_GLOBAL_LOG_FORMAT=json
CBOX_INIT_GLOBAL_LOG_REDACTION_ENABLED=true
CBOX_INIT_GLOBAL_METRICS_ENABLED=true
CBOX_INIT_GLOBAL_API_ENABLED=true
CBOX_INIT_GLOBAL_API_AUTH=$(vault read -field=token secret/cbox-api)
PHP_FPM_AUTOTUNE_PROFILE=heavy
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE=10
```

## Secret Management

### HashiCorp Vault

```bash
#!/bin/bash
# Load secrets from Vault

export CBOX_INIT_GLOBAL_API_AUTH=$(vault kv get -field=api_token secret/cbox)
export DATABASE_PASSWORD=$(vault kv get -field=password secret/database)

exec /usr/local/bin/cbox-init
```

### AWS Secrets Manager

```bash
#!/bin/bash
# Load secrets from AWS Secrets Manager

export CBOX_INIT_GLOBAL_API_AUTH=$(aws secretsmanager get-secret-value \
  --secret-id cbox-api-token \
  --query SecretString \
  --output text)

exec /usr/local/bin/cbox-init
```

### Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cbox-secrets
type: Opaque
data:
  api-token: <base64-encoded-token>
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        env:
          - name: CBOX_INIT_GLOBAL_API_AUTH
            valueFrom:
              secretKeyRef:
                name: cbox-secrets
                key: api-token
```

## Verification

### Check Active Configuration

```bash
# Start with verbose logging
CBOX_INIT_GLOBAL_LOG_LEVEL=debug ./cbox-init

# Check which values are being used (in logs)
# Look for "Configuration loaded" messages
```

### Validate Environment Variables

```bash
#!/bin/bash
# validate-env.sh

required_vars=(
    "CBOX_INIT_GLOBAL_LOG_LEVEL"
    "PHP_FPM_AUTOTUNE_PROFILE"
    "CBOX_INIT_GLOBAL_API_AUTH"
)

for var in "${required_vars[@]}"; do
    if [ -z "${!var}" ]; then
        echo "ERROR: Required variable $var is not set"
        exit 1
    fi
done

echo "All required variables are set"
```

## Troubleshooting

### Environment Variable Not Working

**Check variable name format:**
```bash
# ❌ Wrong
CBOX_INIT_process_nginx_enabled=true

# ✅ Correct
CBOX_INIT_PROCESS_NGINX_ENABLED=true
```

**Verify it's exported:**
```bash
# Check if variable is exported
env | grep CBOX_INIT

# Export if needed
export CBOX_INIT_GLOBAL_LOG_LEVEL=debug
```

### Complex Values (JSON Arrays)

```bash
# Process command as JSON array
CBOX_INIT_PROCESS_APP_COMMAND='["./my-app","--port=8080","--host=0.0.0.0"]'

# Escape quotes properly in shell
CBOX_INIT_PROCESS_APP_COMMAND="[\"./my-app\",\"--port=8080\"]"
```

### Boolean Values

```bash
# All these are treated as true
CBOX_INIT_GLOBAL_METRICS_ENABLED=true
CBOX_INIT_GLOBAL_METRICS_ENABLED=1
CBOX_INIT_GLOBAL_METRICS_ENABLED=yes

# All these are treated as false
CBOX_INIT_GLOBAL_METRICS_ENABLED=false
CBOX_INIT_GLOBAL_METRICS_ENABLED=0
CBOX_INIT_GLOBAL_METRICS_ENABLED=no
```

## See Also

- [Global Settings](global-settings) - Global configuration reference
- [Process Configuration](processes) - Process settings
- [Docker Integration](../getting-started/docker-integration) - Container patterns
- [Examples](../examples/kubernetes) - Kubernetes deployment examples

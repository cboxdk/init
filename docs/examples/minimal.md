---
title: "Minimal Setup"
description: "Simple PHP-FPM configuration for development and testing with Cbox Init"
weight: 30
---

# Minimal Setup Example

The simplest Cbox Init configuration for running PHP-FPM in a container.

## Use Cases

- ✅ Development and local testing
- ✅ Learning Cbox Init basics
- ✅ Single-process PHP applications
- ✅ Proof-of-concept deployments

## Complete Configuration

**File:** `cbox-init.yaml`

```yaml
version: "1.0"

global:
  shutdown_timeout: 30
  log_format: json
  log_level: info

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
```

## Configuration Walkthrough

### Global Settings

```yaml
global:
  shutdown_timeout: 30    # Wait 30 seconds for graceful shutdown
  log_format: json        # Structured logging
  log_level: info         # Standard verbosity
```

**What this does:**
- `shutdown_timeout: 30` - Gives PHP-FPM 30 seconds to finish requests on shutdown
- `log_format: json` - Output JSON logs for log aggregation systems
- `log_level: info` - Show informational messages and above

### PHP-FPM Process

```yaml
processes:
  php-fpm:
    enabled: true                      # Start this process
    command: ["php-fpm", "-F", "-R"]  # Foreground mode, allow root
    restart: always                    # Always restart on exit
```

**Command Breakdown:**
- `php-fpm` - PHP FastCGI Process Manager
- `-F` - Run in foreground (required for process management)
- `-R` - Allow running as root (useful in containers)

**Why `restart: always`:**
- PHP-FPM is critical for handling web requests
- Auto-recovery from crashes or OOM kills
- Ensures service availability

## Dockerfile Integration

```dockerfile
FROM php:8.3-fpm-alpine

# Install Cbox Init
COPY --from=ghcr.io/cboxdk/init:latest /cbox-init /usr/local/bin/cbox-init

# Copy application
WORKDIR /var/www/html
COPY . .

# Copy configuration
COPY cbox-init.yaml /etc/cbox-init/cbox-init.yaml

# Expose PHP-FPM port (for Nginx in separate container)
EXPOSE 9000

# Run Cbox Init as PID 1
ENTRYPOINT ["/usr/local/bin/cbox-init"]
```

## Running the Example

### Local Development

```bash
# Build Cbox Init
make build

# Create minimal configuration
cat > cbox-init.yaml <<'EOF'
version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info
processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
EOF

# Run
./build/cbox-init
```

### Docker

```bash
# Build Docker image
docker build -t myapp:minimal .

# Run container
docker run -p 9000:9000 myapp:minimal
```

### Docker Compose

```yaml
version: '3.8'

services:
  app:
    build: .
    ports:
      - "9000:9000"
    volumes:
      - ./:/var/www/html
```

## Expected Output

```json
{"time":"2024-11-21T10:00:00Z","level":"INFO","msg":"Cbox Init starting","version":"1.0.0"}
{"time":"2024-11-21T10:00:00Z","level":"INFO","msg":"Loading configuration","path":"cbox-init.yaml"}
{"time":"2024-11-21T10:00:00Z","level":"INFO","msg":"Starting process","process":"php-fpm","priority":10}
{"time":"2024-11-21T10:00:01Z","level":"INFO","msg":"Process started successfully","process":"php-fpm","pid":15}
{"time":"2024-11-21T10:00:01Z","level":"INFO","msg":"All processes started"}
```

## Next Steps

Once you have the minimal setup working:

1. **Add Nginx:** See [Laravel Complete](laravel-complete) for web server integration
2. **Add Health Checks:** Monitor PHP-FPM with [Health Checks](../configuration/health-checks)
3. **Enable Auto-Tuning:** Optimize workers with [PHP-FPM Auto-Tuning](../php-fpm-autotune)
4. **Add Monitoring:** Track metrics with [Prometheus](../observability/metrics)

## Customization

### Change Log Level

```bash
# Via environment variable
CBOX_INIT_GLOBAL_LOG_LEVEL=debug ./cbox-init

# Via YAML
global:
  log_level: debug
```

### Adjust Shutdown Timeout

```yaml
global:
  shutdown_timeout: 60  # Give PHP-FPM more time to finish requests
```

### Run as Specific User

```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F"]  # Remove -R flag
    user: www-data              # Run as www-data user
```

## Troubleshooting

### PHP-FPM Won't Start

**Check logs:**
```bash
# Run with debug logging
CBOX_INIT_GLOBAL_LOG_LEVEL=debug ./cbox-init
```

**Common issues:**
- Missing PHP-FPM binary (install `php-fpm` package)
- Port 9000 already in use
- Permission issues (try adding `-R` flag)
- Invalid pool configuration

### Process Keeps Restarting

**Check exit code in logs:**
```json
{"level":"ERROR","msg":"Process exited","process":"php-fpm","exit_code":1}
```

**Solutions:**
- `exit_code: 1` - Configuration error, check PHP-FPM logs
- `exit_code: 137` - OOM killed, reduce workers or increase container memory
- `exit_code: 139` - Segmentation fault, check PHP extensions

## See Also

- [Quick Start](../getting-started/quickstart) - Step-by-step tutorial
- [Laravel Complete](laravel-complete) - Full PHP stack with Nginx (adapt for other frameworks)
- [Process Configuration](../configuration/processes) - All process options
- [Docker Integration](../getting-started/docker-integration) - Docker patterns

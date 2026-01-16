---
title: "PHP-FPM Auto-Tuning"
description: "Intelligent PHP-FPM worker configuration based on container resource limits with OPcache optimization"
weight: 16
---

# PHP-FPM Auto-Tuning Guide

Cbox Init includes intelligent PHP-FPM worker auto-tuning based on container resource limits. This feature eliminates manual calculations and prevents memory over-provisioning that can cause OOM kills.

## Table of Contents

- [Overview](#overview)
- [How It Works](#how-it-works)
- [Application Profiles](#application-profiles)
- [Safety Features](#safety-features)
- [Usage](#usage)
- [Integration with PHP-FPM](#integration-with-php-fpm)
- [Calculation Algorithm](#calculation-algorithm)
- [Troubleshooting](#troubleshooting)

## Overview

PHP-FPM worker configuration is critical for Laravel application performance and stability. Too few workers cause request queuing; too many cause memory exhaustion and container crashes.

**Auto-tuning automatically:**
- Detects container memory and CPU limits (cgroup v1/v2)
- Calculates optimal worker count based on your application profile
- Sets all PM (Process Manager) parameters correctly
- Reserves memory for Nginx, Redis clients, system overhead
- Validates calculations to prevent over-provisioning

## How It Works

### Detection Phase

1. **Container Resource Discovery**
   - Reads cgroup v2: `/sys/fs/cgroup/memory.max`, `/sys/fs/cgroup/cpu.max`
   - Fallback cgroup v1: `/sys/fs/cgroup/memory/memory.limit_in_bytes`, `/sys/fs/cgroup/cpu/cpu.cfs_quota_us`
   - Falls back to host resources if not containerized

2. **Profile Selection**
   - User selects profile via `--php-fpm-profile` flag
   - Profile defines: avg memory per worker, PM mode, spare server ratios

### Calculation Phase

3. **Worker Count Calculation**
   ```
   Available Memory = (Total Memory × MaxMemoryUsage%) - Reserved Memory
   Max Workers = Available Memory ÷ Avg Memory Per Worker
   ```

4. **CPU-Based Limiting**
   ```
   CPU Limit = CPU Cores × 4  (industry standard)
   Max Workers = min(Memory-Based Workers, CPU Limit)
   ```

5. **PM Parameter Calculation** (for dynamic mode)
   ```
   pm.start_servers = max_children × StartServersRatio
   pm.min_spare_servers = max_children × SpareMinRatio
   pm.max_spare_servers = max_children × SpareMaxRatio
   ```

6. **Validation**
   - Total memory usage < container limit
   - PM relationships: `min_spare ≤ start_servers ≤ max_spare ≤ max_children`
   - Minimum workers from profile enforced

### Environment Variable Export

7. **PHP-FPM Integration**
   - Sets environment variables: `PHP_FPM_PM`, `PHP_FPM_MAX_CHILDREN`, etc.
   - PHP-FPM pool config references these via `${VARIABLE}` syntax

## Application Profiles

### Dev Profile
```yaml
Profile: dev
Use Case: Local development
Workers: 2 (static)
Memory/Worker: ~32MB (runtime + request overhead)
OPcache (shared): 64MB (compiled app code shared by all workers)
Reserved Memory: 64MB
Max Memory Usage: 50%
PM Mode: static
Total Memory: 2 × 32MB + 64MB + 64MB = 192MB
```

**When to use:**
- Local development with Docker Desktop
- Fast startup, minimal footprint
- Debugging-friendly (predictable worker count)

**How OPcache reduces memory:** App code is compiled once and stored in shared OPcache (64MB), not loaded in each worker. Workers only need runtime + request memory (~32MB each).

**Example:** 512MB container → 2 workers (uses ~192MB total)

---

### Light Profile
```yaml
Profile: light
Use Case: Small apps, low traffic (1-10 req/s)
Workers: Auto-calculated
Memory/Worker: ~36MB (runtime + request overhead)
OPcache (shared): 96MB (compiled app code shared by all workers)
Reserved Memory: 128MB
Max Memory Usage: 70%
PM Mode: dynamic
Spare Min/Max: 25% / 50%
```

**When to use:**
- Small Laravel apps, internal tools
- Cost-optimized cloud deployments
- Background job processors

**How OPcache reduces memory:** Small Laravel app with some packages compiles to ~96MB opcodes in shared OPcache. Each worker only needs ~36MB for runtime + request handling.

**Example:** 1GB container → ~14 workers (700MB available - 224MB reserved = 476MB ÷ 36MB)

---

### Medium Profile
```yaml
Profile: medium (RECOMMENDED)
Use Case: Standard production (10-50 req/s)
Workers: Auto-calculated
Memory/Worker: ~42MB (runtime + request overhead)
OPcache (shared): 128MB (compiled app code shared by all workers)
Reserved Memory: 192MB
Max Memory Usage: 75%
PM Mode: dynamic
Spare Min/Max: 25% / 50%
```

**When to use:**
- Most Laravel production applications
- Balanced performance and resource efficiency
- APIs with moderate traffic

**How OPcache reduces memory:** Standard Laravel with packages compiles to ~128MB opcodes in shared OPcache. Each worker only needs ~42MB for runtime + request handling, allowing 2-3x more workers!

**Example:** 2GB container → ~16 workers (CPU limited at 4 cores × 4 = 16)
- Without OPcache: ~10 workers at 80MB each
- With OPcache: 16 workers at 42MB each + 128MB shared

---

### Heavy Profile
```yaml
Profile: heavy
Use Case: High traffic (50-200 req/s)
Workers: Auto-calculated (minimum 8)
Memory/Worker: ~52MB (runtime + request overhead)
OPcache (shared): 256MB (compiled app code shared by all workers)
Reserved Memory: 384MB
Max Memory Usage: 80%
PM Mode: dynamic
Spare Min/Max: 20% / 40%
```

**When to use:**
- High-traffic Laravel applications
- Large apps with many packages and dependencies
- Performance-critical APIs

**How OPcache reduces memory:** Large Laravel app with many packages compiles to ~256MB opcodes in shared OPcache. Workers need more overhead for connections/caching (~52MB) but still much less than without OPcache!

**Example:** 8GB container → ~32 workers (CPU limited at 8 cores × 4 = 32)
- Without OPcache: ~12 workers at 128MB each
- With OPcache: 32 workers at 52MB each + 256MB shared = 2.5x more workers!

---

### Bursty Profile
```yaml
Profile: bursty
Use Case: Variable traffic with spikes
Workers: Auto-calculated (minimum 4)
Memory/Worker: ~44MB (runtime + request overhead)
OPcache (shared): 128MB (compiled app code shared by all workers)
Reserved Memory: 192MB
Max Memory Usage: 75%
PM Mode: dynamic
Spare Min/Max: 40% / 70%
Start Servers: 50% of max
```

**When to use:**
- E-commerce sites (flash sales)
- Event-driven traffic patterns
- Applications with unpredictable load

**How OPcache reduces memory:** Similar to medium profile (~44MB per worker) but with aggressive spare settings to handle traffic spikes quickly. More workers = better spike handling!

**Example:** 4GB container → ~30 workers (12 min spare, 21 max spare, 15 start)
- Without OPcache: ~12 workers at 96MB each
- With OPcache: 30 workers at 44MB each + 128MB shared = 2.5x more capacity for spikes!

## Safety Features

### Memory Protection
- **Never exceeds container limit**: Total usage = workers + reserved < limit
- **Reserved memory**: Nginx (varies), Redis/MySQL clients, system overhead
- **Safety margin**: Max 50-80% memory usage (profile-dependent)

### CPU Protection
- **Max 4 workers per CPU core**: Industry-standard ratio
- **Prevents context switching**: Too many workers on limited CPUs degrade performance

### Validation Gates
- **Pre-calculation checks**: Minimum memory requirements per profile
- **Post-calculation validation**: PM relationships, memory limits
- **Warning system**: Logs adjustments (CPU limiting, profile minimums)

### Profile Minimums
- Each profile enforces minimum worker count
- Prevents under-provisioning on small containers
- Dev: 2 workers, Light: 2, Medium: 4, Heavy: 8, Bursty: 4

## Usage

### Basic Usage

```bash
# Via CLI flag
./build/cbox-init --php-fpm-profile=medium

# Via environment variable (recommended for containers)
PHP_FPM_AUTOTUNE_PROFILE=medium ./build/cbox-init

# Docker
docker run -e PHP_FPM_AUTOTUNE_PROFILE=medium myapp:latest

# Priority: CLI flag > ENV var
PHP_FPM_AUTOTUNE_PROFILE=light ./build/cbox-init --php-fpm-profile=heavy
# Result: Uses 'heavy' (CLI overrides ENV)
```

### With Config File

```bash
# Combine with specific config
./build/cbox-init \
  --php-fpm-profile=medium \
  --config /etc/cbox-init/production.yaml
```

### Docker Integration

**Dockerfile:**
```dockerfile
FROM php:8.3-fpm-alpine

# Install cbox-init
COPY build/cbox-init /usr/local/bin/cbox-init

# PHP-FPM pool config with environment variable placeholders
COPY www.conf /usr/local/etc/php-fpm.d/www.conf

# Default autotune profile (can be overridden at runtime)
ENV PHP_FPM_AUTOTUNE_PROFILE=medium

# Start with auto-tuning enabled via ENV
CMD ["cbox-init", "--config", "/etc/cbox-init/cbox-init.yaml"]
```

### Docker Compose

```yaml
services:
  app:
    image: myapp:latest
    environment:
      # Auto-tune PHP-FPM based on container limits
      - PHP_FPM_AUTOTUNE_PROFILE=medium
    deploy:
      resources:
        limits:
          memory: 2G      # Auto-tuner uses this
          cpus: '2'       # Auto-tuner uses this
    # No need to specify --php-fpm-profile in command
    # ENV var activates it automatically

  app-heavy:
    image: myapp:latest
    environment:
      # Different profile for high-traffic instance
      - PHP_FPM_AUTOTUNE_PROFILE=heavy
    deploy:
      resources:
        limits:
          memory: 8G
          cpus: '8'
```

**Kubernetes Deployment:**
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
        image: myapp:latest
        env:
        - name: PHP_FPM_AUTOTUNE_PROFILE
          value: "medium"
        # Or from ConfigMap:
        # - name: PHP_FPM_AUTOTUNE_PROFILE
        #   valueFrom:
        #     configMapKeyRef:
        #       name: app-config
        #       key: php_fpm_profile
        resources:
          limits:
            memory: "2Gi"
            cpu: "2"
```

### Validation

```bash
# Test autotune without starting processes
./build/cbox-init --php-fpm-profile=medium --dry-run

# Output:
# 🎯 PHP-FPM auto-tuned (medium profile):
#    pm = dynamic
#    pm.max_children = 6
#    pm.start_servers = 2
#    pm.min_spare_servers = 1
#    pm.max_spare_servers = 3
#    pm.max_requests = 1000
#    Memory: 1536MB allocated / 2048MB total
```

## Integration with PHP-FPM

### Pool Configuration

Cbox Init exports environment variables that PHP-FPM can reference in `www.conf`:

```ini
[www]
; Use auto-tuned values via environment variables
pm = ${PHP_FPM_PM}
pm.max_children = ${PHP_FPM_MAX_CHILDREN}
pm.start_servers = ${PHP_FPM_START_SERVERS}
pm.min_spare_servers = ${PHP_FPM_MIN_SPARE}
pm.max_spare_servers = ${PHP_FPM_MAX_SPARE}
pm.max_requests = ${PHP_FPM_MAX_REQUESTS}

; Standard pool settings
pm.process_idle_timeout = 10s
pm.max_requests = ${PHP_FPM_MAX_REQUESTS}
```

### Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `PHP_FPM_PM` | Process manager mode | `dynamic` |
| `PHP_FPM_MAX_CHILDREN` | Maximum workers | `10` |
| `PHP_FPM_START_SERVERS` | Workers to start | `3` |
| `PHP_FPM_MIN_SPARE` | Minimum idle workers | `2` |
| `PHP_FPM_MAX_SPARE` | Maximum idle workers | `5` |
| `PHP_FPM_MAX_REQUESTS` | Requests before restart | `1000` |

### Manual Override

You can still manually override if needed:

```bash
# Override auto-tuned values
export PHP_FPM_MAX_CHILDREN=20
./build/cbox-init --php-fpm-profile=medium
```

## Calculation Algorithm

### Detailed Example: Medium Profile, 2GB Container, 4 CPUs

```
1. Container Limits:
   - Memory: 2048MB
   - CPUs: 4

2. Profile Configuration (medium):
   - Avg Memory/Worker: 256MB
   - Reserved Memory: 512MB
   - Max Memory Usage: 75%
   - Spare Min Ratio: 0.25
   - Spare Max Ratio: 0.5
   - Start Servers Ratio: 0.33

3. Memory-Based Calculation:
   Available = 2048MB × 0.75 = 1536MB
   Worker Memory = 1536MB - 512MB reserved = 1024MB
   Workers = 1024MB ÷ 256MB = 4 workers

4. CPU-Based Limit:
   CPU Limit = 4 cores × 4 = 16 workers
   Final = min(4, 16) = 4 workers  (memory-limited)

5. PM Parameters (dynamic):
   pm.max_children = 4
   pm.start_servers = ceil(4 × 0.33) = 2
   pm.min_spare_servers = ceil(4 × 0.25) = 1
   pm.max_spare_servers = ceil(4 × 0.5) = 2
   pm.max_requests = 1000

6. Validation:
   Total Memory = (4 × 256MB) + 512MB = 1536MB ✓
   1536MB < 2048MB limit ✓
   PM: 1 ≤ 2 ≤ 2 ≤ 4 ✓
```

## Troubleshooting

### Error: "insufficient memory: 0MB"

**Cause:** Container limits not detected (not running in container or cgroup not accessible)

**Solution:**
- Run in actual Docker container with memory limit
- Check cgroup mount: `ls /sys/fs/cgroup/`
- Manual fallback: Don't use `--php-fpm-profile`, configure PHP-FPM manually

### Error: "insufficient memory for workers"

**Cause:** Container too small for selected profile

**Solutions:**
1. Increase container memory: `docker run -m 2G ...`
2. Use lighter profile: `--php-fpm-profile=light` or `dev`
3. Reduce reserved memory (not recommended)

### Warning: "Memory allows X workers, but limiting to Y based on CPUs"

**Meaning:** You have more memory than CPUs can handle efficiently

**Action:** This is safe - CPU limit prevents context switching overhead. Consider:
- Increasing CPU allocation if latency-sensitive
- Accepting the limit if throughput is adequate

### Warning: "Calculated X workers, but profile limits to Y"

**Meaning:** Profile enforces maximum (e.g., dev profile = 2 workers max)

**Action:** Use correct profile for your environment:
- Dev: Local development only
- Light/Medium/Heavy: Production profiles

### Workers dying with OOM

**Diagnosis:**
```bash
# Check actual memory usage
docker stats

# Check PHP-FPM memory_limit
php -i | grep memory_limit
```

**Solutions:**
1. Reduce `memory_limit` in `php.ini` (e.g., `256M` → `128M`)
2. Use heavier profile with more memory/worker: `--php-fpm-profile=heavy`
3. Increase container memory limit
4. Profile your app to find memory leaks

### Too few workers (requests queuing)

**Diagnosis:**
```bash
# Check PHP-FPM status
docker exec app kill -USR2 1  # Reload PHP-FPM
curl http://localhost/status?full
```

**Solutions:**
1. Increase container memory to get more workers
2. Switch to lighter profile: `--php-fpm-profile=light` (more workers, less memory each)
3. Optimize app memory usage (caching, DB queries)
4. Add horizontal scaling (more containers)

## Best Practices

### Profile Selection
- **Start with `medium`** for most Laravel apps
- **Use `dev`** only for local development
- **Upgrade to `heavy`** if >50 req/s and you have large memory limits
- **Use `bursty`** for e-commerce, events, unpredictable traffic

### Container Sizing
- Minimum recommendations by profile:
  - Dev: 384MB
  - Light: 768MB
  - Medium: 2GB
  - Heavy: 4GB+
  - Bursty: 4GB+

### Monitoring
- Track `pm.status_path` metrics (active workers, queue length)
- Alert on memory usage >90%
- Monitor worker churn (restarts from `pm.max_requests`)

### Testing
- Always `--dry-run` before production deployment
- Load test with realistic traffic patterns
- Verify no OOM kills under peak load
- Check PM status during traffic spikes

### PHP Configuration
- Set `memory_limit` conservatively (profile avg memory - 20%)
- Enable OPcache with sufficient memory
- Configure `max_execution_time` appropriately
- Use `pm.status_path` for monitoring

## Advanced: Custom Profiles

While not exposed via CLI, you can create custom profiles by modifying `internal/autotune/profiles.go`:

```go
ProfileCustom: {
    Name:                "Custom Production",
    Description:         "Tailored for our specific app",
    ProcessManagerType:  "dynamic",
    AvgMemoryPerWorker:  384,  // Measured from app profiling
    MinWorkers:          6,
    MaxWorkers:          0,     // Auto-calculate
    SpareMinRatio:       0.3,
    SpareMaxRatio:       0.6,
    StartServersRatio:   0.4,
    MaxRequestsPerChild: 1500,
    MaxMemoryUsage:      0.75,
    ReservedMemoryMB:    768,   // Nginx + Redis + MySQL clients
},
```

Then rebuild and use: `--php-fpm-profile=custom`

---

**Need help?** Open an issue at https://github.com/cboxdk/init/issues

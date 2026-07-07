---
title: "Container Readiness"
description: "Configure Kubernetes readiness probes using file-based health indicators"
weight: 28
---

# Container Readiness

Cbox Init supports file-based readiness indicators for Kubernetes integration. When all tracked processes are ready, a readiness file is created. Kubernetes readiness probes can check for this file's existence to determine if the container is ready to receive traffic.

## Overview

The readiness feature:

- Creates a file when all tracked processes are healthy/running
- Removes the file when any tracked process becomes unhealthy
- Supports two modes: `all_healthy` (default) and `all_running`
- Works seamlessly with Kubernetes `exec` readiness probes
- Automatically cleans up on shutdown

## Quick Start

```yaml
version: "1.0"
global:
  readiness:
    enabled: true
    path: "/tmp/cbox-ready"
    mode: "all_healthy"

processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    depends_on: [php-fpm]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
```

## Configuration Reference

### global.readiness

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | `boolean` | `false` | Enable/disable readiness file management |
| `path` | `string` | `/tmp/cbox-ready` | Path to the readiness file |
| `mode` | `string` | `all_healthy` | Readiness evaluation mode |
| `content` | `string` | `ready\ntimestamp=...` | Custom file content when ready |
| `processes` | `[]string` | `[]` | Specific processes to track (empty = all) |
| `http_port` | `int` | `0` | Serve `/readyz` + `/livez` on this port (`0` = disabled) |
| `http_host` | `string` | `0.0.0.0` | Bind host for the HTTP endpoints |

## HTTP probes (active readiness)

The readiness **file** is passive: if cbox-init hangs while still alive, a stale file keeps
reporting "ready". Set `http_port` to also expose an **active** signal served by cbox-init itself —
if the supervisor wedges, the endpoint stops answering and the probe fails.

```yaml
global:
  readiness:
    enabled: true
    http_port: 9091      # exposes /readyz + /livez
```

- `GET /readyz` → `200` when all tracked processes are ready, `503` otherwise (JSON body lists each
  process's state). Use for the **readinessProbe** (traffic gating).
- `GET /livez` → `200` whenever cbox-init can answer. Use for the **livenessProbe** (restart the pod
  only when the supervisor itself is unresponsive, not merely when the app is unready).

```yaml
readinessProbe:
  httpGet: { path: /readyz, port: 9091 }
  periodSeconds: 5
livenessProbe:
  httpGet: { path: /livez, port: 9091 }
  periodSeconds: 10
  failureThreshold: 3
```

Prefer HTTP probes on Kubernetes: they detect a wedged supervisor and avoid host-networking / CNI
source-IP quirks that can defeat localhost ACLs on nginx-served health paths. The `exec` file-probe
remains fully supported and complementary.

### Readiness Modes

#### all_healthy (default)

The container is ready only when all tracked processes have passed their health checks.

```yaml
global:
  readiness:
    enabled: true
    mode: "all_healthy"
```

**Requirements:**
- Processes with health checks must pass them
- Processes without health checks are considered healthy if running

#### all_running

The container is ready when all tracked processes are running, regardless of health check status.

```yaml
global:
  readiness:
    enabled: true
    mode: "all_running"
```

**Use when:**
- Health checks are not critical for traffic routing
- You want faster readiness (before health checks pass)
- Processes don't have meaningful health endpoints

## Kubernetes Integration

### Pod Manifest Example

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: php-app
spec:
  containers:
    - name: app
      image: your-app:latest
      command: ["/usr/local/bin/cbox-init"]
      readinessProbe:
        exec:
          command:
            - test
            - -f
            - /tmp/cbox-ready
        initialDelaySeconds: 5
        periodSeconds: 5
        failureThreshold: 3
      livenessProbe:
        exec:
          command:
            - pgrep
            - -f
            - cbox-init
        initialDelaySeconds: 10
        periodSeconds: 10
```

### Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: php
  template:
    metadata:
      labels:
        app: php
    spec:
      containers:
        - name: app
          image: your-app:latest
          ports:
            - containerPort: 80
          readinessProbe:
            exec:
              command: ["test", "-f", "/tmp/cbox-ready"]
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            limits:
              memory: "512Mi"
              cpu: "500m"
```

## Process Filtering

Track only specific processes for readiness evaluation:

```yaml
global:
  readiness:
    enabled: true
    path: "/tmp/cbox-ready"
    mode: "all_healthy"
    processes:
      - php-fpm
      - nginx
      # horizon excluded - not critical for traffic routing

processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    health_check:
      type: tcp
      address: "127.0.0.1:9000"

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"

  horizon:
    command: ["php", "artisan", "horizon"]
    # Not included in readiness check
```

## Custom Content

Provide custom content for the readiness file:

```yaml
global:
  readiness:
    enabled: true
    path: "/tmp/cbox-ready"
    content: |
      OK
      version=1.0.0
      service=php-app
```

Default content (when not specified):
```
ready
timestamp=1732789012
```

## State Transitions

### Process States and Readiness

| Process State | Health | all_healthy | all_running |
|--------------|--------|-------------|-------------|
| running | healthy | Ready | Ready |
| running | unknown | Not Ready | Ready |
| running | unhealthy | Not Ready | Not Ready |
| healthy | - | Ready | Ready |
| stopped | - | Not Ready | Not Ready |
| failed | - | Not Ready | Not Ready |

### File Lifecycle

1. **Startup**: Readiness file does not exist
2. **All processes ready**: File is created at configured path
3. **Any process unhealthy**: File is removed
4. **Process recovery**: File is recreated when all processes recover
5. **Shutdown**: File is removed during graceful shutdown

## Complete Example

```yaml
version: "1.0"
global:
  shutdown_timeout: 30
  log_level: info
  readiness:
    enabled: true
    path: "/tmp/cbox-ready"
    mode: "all_healthy"
    processes:
      - php-fpm
      - nginx

processes:
  php-fpm:
    enabled: true
    command: ["php-fpm", "-F", "-R"]
    restart: always
    health_check:
      type: tcp
      address: "127.0.0.1:9000"
      interval: 10
      timeout: 5
      retries: 3

  nginx:
    enabled: true
    command: ["nginx", "-g", "daemon off;"]
    restart: always
    depends_on: [php-fpm]
    health_check:
      type: http
      address: "http://127.0.0.1:80/health"
      interval: 10
      timeout: 5
      retries: 3

  horizon:
    enabled: true
    command: ["php", "artisan", "horizon"]
    restart: on-failure
    depends_on: [php-fpm]
    # Not tracked for readiness - background worker
```

## Troubleshooting

### Readiness file not created

1. **Check if readiness is enabled:**
   ```bash
   grep -A5 "readiness:" cbox-init.yaml
   ```

2. **Verify all tracked processes are healthy:**
   ```bash
   curl -s http://localhost:9180/api/v1/processes | jq '.processes[] | {name, state, health}'
   ```

3. **Check logs for readiness manager:**
   ```bash
   # Look for "Container is ready" or "Container is not ready" messages
   ```

### File exists but K8s reports not ready

1. **Verify probe path matches config:**
   ```yaml
   # Cbox config
   readiness:
     path: "/tmp/cbox-ready"  # Must match K8s probe path
   ```

2. **Check file permissions:**
   ```bash
   ls -la /tmp/cbox-ready
   ```

3. **Test probe manually in container:**
   ```bash
   kubectl exec -it pod-name -- test -f /tmp/cbox-ready && echo "Ready" || echo "Not ready"
   ```

### Container stays not-ready after processes start

- Ensure health checks are configured correctly
- Check `mode: "all_running"` if health checks are slow
- Verify the `processes` list includes only critical services

## Best Practices

1. **Track only traffic-critical processes** in `processes` list
2. **Use `all_healthy` mode** for production deployments
3. **Configure appropriate health check intervals** (5-10 seconds)
4. **Set K8s probe `failureThreshold`** to allow for transient failures
5. **Use `/tmp/` or memory-backed paths** for faster I/O

## See Also

- [Health Checks](health-checks) - Configure health monitoring
- [Dependency Management](dependency-management) - Process startup ordering
- [Configuration Overview](../configuration/overview) - All configuration options

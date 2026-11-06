---
title: "Management API"
description: "Control and inspect Cbox Init processes via REST API at runtime"
weight: 42
---

# Management API

REST API for managing and inspecting processes at runtime.

> **OpenAPI spec:** a machine-readable OpenAPI 3.0 definition of every endpoint
> lives at [`openapi.yaml`](openapi.yaml) — load it into Swagger UI, Postman, or
> an SDK generator.

## Configuration

Enable the API in your `cbox-init.yaml`:

```yaml
global:
  api_enabled: true
  api_port: 9180
  api_auth: "your-secure-token-here"  # Optional Bearer token
```

## Authentication

If `api_auth` is configured, all API requests (except `/api/v1/health`) require Bearer token authentication:

```bash
curl -H "Authorization: Bearer your-secure-token-here" \
  http://localhost:9180/api/v1/processes
```

## Endpoints

### Snapshot sleep and wake

The qualified warm tier exposes these routes through the same management
listeners and authentication as process controls:

| Method | Path | Result |
| --- | --- | --- |
| GET | `/api/v1/snapshot` | Observe state without waking or refreshing idle time |
| POST | `/api/v1/snapshot/sleep` | Checkpoint now if the workload is idle and safe |
| POST | `/api/v1/snapshot/wake` | Restore through the ordinary recovery path |

The commands use the local Unix socket by default. `--url` selects an explicit
management endpoint and the usual API authentication configuration applies.

```bash
cbox-init snapshot status
cbox-init snapshot sleep
cbox-init snapshot wake
```

Successful responses are JSON with `Cache-Control: no-store`:

```json
{"supported":true,"state":"asleep","idle_seconds":300}
```

States are `awake`, `asleep`, `transitioning`, `disabled`, `unknown`, and
`unavailable`. `supported` means a qualified coordinator was attached at startup;
`disabled_reason`, when present, explains why it stopped sleeping. An unavailable
coordinator reports `supported: false`. A closed proxy alone is never reported as
a completed checkpoint. State is an observation and can change as traffic arrives.

Sleep skips only the idle timer. It refuses open connections, active requests,
unconfirmed native PHP-FPM work, or disabled sleep. Neither mutation accepts a
body, query options, or a force override. Busy/unavailable returns 409; invalid
options return 400; a bounded operation timeout returns 504; other failures return
503. After an uncertain failure, observe runtime and orchestrator state before
retrying. The runtime may fence itself; a lost checkpoint can cold-start through
the existing recovery path, with the loss reported in logs and `disabled_reason`.

Operations have a 60-second control budget, a 70-second HTTP write budget, and a
75-second CLI client timeout. They inherit the caller's earlier cancellation.
Repeated completed actions are harmless, but clients do not automatically retry
mutations. New traffic can wake a sleeping workload immediately.

This is process checkpointing inside the existing container. It does not remove
the pod, release its reserved CPU/RAM, change instance count, or establish billable
usage. A compatible, qualified snapshot agent and runtime configuration are
required. Without them the workload stays awake and mutations are refused.

### Health Check

**GET** `/api/v1/health`

Returns API health status (no authentication required).

**Response:**
```json
{
  "status": "healthy"
}
```

### List Processes

**GET** `/api/v1/processes`

Returns status of all managed processes.

**Response:**
```json
{
  "processes": [
    {
      "name": "php-fpm",
      "state": "running",
      "scale": 2,
      "instances": [
        {
          "id": "php-fpm-0",
          "state": "running",
          "pid": 1234,
          "started_at": 1700000000,
          "restart_count": 0
        },
        {
          "id": "php-fpm-1",
          "state": "running",
          "pid": 1235,
          "started_at": 1700000001,
          "restart_count": 0
        }
      ]
    }
  ]
}
```

### Process Actions

**POST** `/api/v1/processes/{name}/{action}`

Perform actions on a specific process.

#### Restart Process

**POST** `/api/v1/processes/php-fpm/restart`

**Response:**
```json
{
  "message": "restart initiated",
  "process": "php-fpm"
}
```

#### Stop Process

**POST** `/api/v1/processes/php-fpm/stop`

**Response:**
```json
{
  "message": "stop initiated",
  "process": "php-fpm"
}
```

#### Start Process

**POST** `/api/v1/processes/php-fpm/start`

**Response:**
```json
{
  "message": "start initiated",
  "process": "php-fpm"
}
```

#### Scale Process

**POST** `/api/v1/processes/php-fpm/scale`

**Request Body:**
```json
{
  "scale": 5
}
```

**Response:**
```json
{
  "message": "scale initiated",
  "process": "php-fpm",
  "scale": 5
}
```

## Examples

### List all processes

```bash
curl -H "Authorization: Bearer your-token" \
  http://localhost:9180/api/v1/processes
```

### Restart a process

```bash
curl -X POST \
  -H "Authorization: Bearer your-token" \
  http://localhost:9180/api/v1/processes/php-fpm/restart
```

### Scale a process to 10 instances

```bash
curl -X POST \
  -H "Authorization: Bearer your-token" \
  -H "Content-Type: application/json" \
  -d '{"scale": 10}' \
  http://localhost:9180/api/v1/processes/queue-default/scale
```

### Check API health (no auth required)

```bash
curl http://localhost:9180/api/v1/health
```

## Error Responses

### 401 Unauthorized

```json
{
  "error": "unauthorized"
}
```

### 400 Bad Request

```json
{
  "error": "invalid request body"
}
```

### 405 Method Not Allowed

```json
{
  "error": "method not allowed"
}
```

## Notes

- Process actions (restart, stop, start, scale) return immediately with `202 Accepted`
- Actual state changes happen asynchronously
- Use `GET /api/v1/processes` to poll for current state
- Dynamic scaling requires supervisor support (Phase 6+)

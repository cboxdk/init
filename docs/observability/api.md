---
title: "Management API"
description: "Control and inspect Cbox Init processes via REST API at runtime"
weight: 42
---

# Management API

REST API for managing and inspecting processes at runtime.

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

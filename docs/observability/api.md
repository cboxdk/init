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
`unavailable`. `supported` means a qualified coordinator was attached at startup.
An unavailable coordinator reports `supported: false`. A closed proxy alone is
never reported as a completed checkpoint. State is an observation and can change
as traffic arrives.

#### Why something happened

Every checkpoint or restore that is not a plain success reports a
machine-readable `cause`, a `level`, and a one-sentence `message` that says what
happened, why, and what happens next. These clear after the next success;
`last_event` keeps the most recent one with its full detail. The reason is never
only in the logs, and the log line for the same outcome carries the same
`cause` field at the same level.

```json
{
  "supported": true,
  "state": "awake",
  "idle_seconds": 300,
  "last_restore": "cold_start",
  "cause": "checkpoint_missing",
  "level": "info",
  "message": "no checkpoint images on this node to restore (...); cold started the workload in place and warm sleep stays on, so the next sleep takes a fresh checkpoint",
  "last_event": {
    "operation": "restore",
    "cause": "checkpoint_missing",
    "level": "info",
    "message": "...",
    "agent_reason": "images_missing",
    "at": "2026-11-09T10:00:00Z"
  }
}
```

| Field | Meaning |
|-------|---------|
| `last_restore` | `restored` (the checkpoint loaded) or `cold_start` (the workload was started again in place), once a wake has happened |
| `cause`, `level`, `message` | The most recent operation, when it was not a plain success |
| `disabled_reason` | A sentence saying why warm sleep is off for this container |
| `disabled_cause` | The cause code for `disabled_reason` |
| `last_event` | The most recent non-success outcome: `operation`, `cause`, `level`, `message`, `agent_reason`, `criu_exit`, `criu_log`, `attempt`, `at` |

Cause codes are stable; new ones may be added.

| Cause | Level | What happened | Warm sleep |
|-------|-------|---------------|------------|
| `checkpoint_missing` | info | No checkpoint images on this node (node replaced or rebooted, images swept, pod moved). Cold started. | Stays on; the next sleep takes a fresh checkpoint |
| `checkpoint_incompatible` | warning | Images were written for another kernel, CRIU or image build. Cold started. | Stays on |
| `restore_failed` | error | One restore of a present checkpoint failed. Images kept, still asleep. | Stays on; the next connection retries |
| `restore_abandoned` | error | A present checkpoint would not load and the agent gave up on it. Cold started. | Stays on for one fresh checkpoint; a second in a row turns it off |
| `checkpoint_failed` | error | A dump failed; the workload kept running. | Off after 3 in a row |
| `agent_disabled` | error | The node agent no longer accepts checkpoints for this container. | Off |
| `control_lost` | error | The channel to the node agent is gone. | Off |
| `cold_start_failed` | error | Starting the workload again after a lost checkpoint failed. | Off |
| `operation_uncertain` | error | The outcome of a sent operation is unknown; the container fences itself. | Off |
| `agent_refused` | error | The agent refused with a reason this version does not know; see `agent_reason`. | Retries |
| `idle_not_configured` | — | No idle timeout is set (reported as `disabled_cause` only). | Off |

`criu_exit` and `criu_log` appear when the node agent reports CRIU's exit status
and log line. Without them, the message says the agent sent no diagnosis and
that its log for the container has it. A node agent supplies them as optional
fields on its `ERR` reply, percent-encoded so each stays one token:
`ERR reason=restore_failed criu_exit=1 criu_log=Error%20(criu/cr-restore.c:42):%20...`.
An agent may also send `reason=checkpoint_incompatible` and a free-text
`detail=`. Agents that send only `reason` keep working.

Sleep skips only the idle timer. It refuses open connections, active requests,
unconfirmed native PHP-FPM work, or disabled sleep. Neither mutation accepts a
body, query options, or a force override. Busy/unavailable returns 409; invalid
options return 400; a bounded operation timeout returns 504; other failures return
503. Error responses carry `error` (a sentence), `cause`, and the observed
`snapshot` status:

```json
{"error":"workload has active or unconfirmed work: 2 open connections, 0 requests in flight","cause":"workload_busy","snapshot":{"supported":true,"state":"awake","idle_seconds":300}}
```

Refusal causes are `workload_busy`, `sleep_unavailable` (or the
`disabled_cause` when warm sleep is off), and `operation_timeout`; failures use
the outcome cause above. After an uncertain failure, observe runtime and
orchestrator state before retrying. The runtime may fence itself.

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

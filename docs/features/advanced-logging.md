---
title: "Advanced Logging"
description: "Automatic log level detection, multiline handling, JSON parsing, and sensitive data redaction"
weight: 26
---

# Advanced Logging

Cbox Init provides enterprise-grade log processing with automatic level detection, multiline handling, JSON parsing, and sensitive data redaction.

## Features

- ✅ **Automatic log level detection:** Parse levels from various formats
- ✅ **Multiline log handling:** Reassemble stack traces automatically
- ✅ **JSON log parsing:** Extract structured fields from JSON logs
- ✅ **Sensitive data redaction:** Prevent credential leaks
- ✅ **Log filtering:** Filter by level or pattern
- ✅ **Per-process segmentation:** Label and filter logs by process

## Automatic Log Level Detection

### Supported Formats

Cbox Init automatically detects log levels from:

```
[ERROR] Database connection failed      → ERROR
2024-11-20 ERROR: Query timeout         → ERROR
{"level":"warn","msg":"Slow query"}     → WARN
php artisan: INFO - Cache cleared       → INFO
ERROR: Something went wrong             → ERROR
[2024-11-20 10:00:00] production.ERROR  → ERROR
```

**Detected Levels:**
- ERROR
- WARN / WARNING
- INFO / INFORMATION
- DEBUG
- TRACE
- FATAL
- CRITICAL

### Configuration

```yaml
global:
  log_level_detection: true  # Default: true
```

**Output:**
```json
{
  "time": "2024-11-21T10:00:00Z",
  "level": "ERROR",
  "process": "php-fpm",
  "msg": "Database connection failed"
}
```

## Multiline Log Handling

### Problem

Stack traces and multi-line errors get split:

```
❌ Without multiline handling:
{"level":"ERROR","msg":"[ERROR] Exception in Controller"}
{"level":"INFO","msg":"    at App\\Http\\Controllers\\UserController->store()"}
{"level":"INFO","msg":"    at Illuminate\\Routing\\Controller->callAction()"}
```

### Solution

Cbox Init automatically reassembles multiline logs:

```
✅ With multiline handling:
{
  "level": "ERROR",
  "msg": "[ERROR] Exception in Controller\n    at App\\Http\\Controllers\\UserController->store()\n    at Illuminate\\Routing\\Controller->callAction()"
}
```

### Configuration

```yaml
global:
  log_multiline_enabled: true
  log_multiline_pattern: '^\\[|^\\d{4}-|^{"'  # Regex: lines starting with [, date, or {
  log_multiline_timeout: 500  # milliseconds
  log_multiline_max_lines: 100
```

**Pattern Explanation:**
- `^\\[` - Lines starting with `[` (e.g., `[ERROR]`)
- `^\\d{4}-` - Lines starting with year (e.g., `2024-11-20`)
- `^{"` - Lines starting with `{` (JSON logs)

**How it works:**
1. Process reads line from stderr/stdout
2. If line matches pattern → Start new log entry
3. If line doesn't match → Append to current entry
4. After timeout (500ms) → Flush current entry

### Examples

**PHP Stack Trace:**
```php
// Input (raw):
[ERROR] Exception in UserController
    at App\Http\Controllers\UserController->store()
    at Illuminate\Routing\Controller->callAction()
    at Illuminate\Routing\ControllerDispatcher->dispatch()

// Output (combined):
{
  "level": "ERROR",
  "process": "php-fpm",
  "msg": "[ERROR] Exception in UserController\n    at App\\Http\\Controllers\\UserController->store()\n    at Illuminate\\Routing\\Controller->callAction()\n    at Illuminate\\Routing\\ControllerDispatcher->dispatch()"
}
```

**Laravel Log:**
```php
// Input:
[2024-11-21 10:00:00] production.ERROR: Database query failed
Stack trace:
#0 /var/www/vendor/laravel/framework/src/Database/Connection.php(123)
#1 /var/www/app/Models/User.php(45)

// Output (combined):
{
  "time": "2024-11-21T10:00:00Z",
  "level": "ERROR",
  "msg": "[2024-11-21 10:00:00] production.ERROR: Database query failed\nStack trace:\n#0 /var/www/vendor/laravel/framework/src/Database/Connection.php(123)\n#1 /var/www/app/Models/User.php(45)"
}
```

## JSON Log Parsing

### Automatic Field Extraction

Cbox Init parses JSON logs and extracts structured fields:

**Input (JSON from application):**
```json
{"level":"error","msg":"Query failed","query":"SELECT *","duration":5000}
```

**Output (enriched):**
```json
{
  "time": "2024-11-21T10:00:00Z",
  "level": "ERROR",
  "process": "php-fpm",
  "msg": "Query failed",
  "query": "SELECT *",
  "duration": 5000
}
```

### Configuration

```yaml
global:
  log_json_parsing: true  # Default: true
```

### Field Mapping

**Common JSON log formats:**

Laravel:
```json
{"message":"User created","context":{"user_id":123},"level":"info"}
```

Monolog:
```json
{"message":"Request processed","context":{"duration":150},"level_name":"INFO"}
```

Custom:
```json
{"msg":"API call","method":"POST","path":"/users","status":201}
```

**All are parsed and standardized.**

## Sensitive Data Redaction

### Automatic Redaction

Cbox Init automatically redacts sensitive information:

**Redacted Patterns:**
- Passwords: `password`, `passwd`, `pwd`
- API tokens: `token`, `api_key`, `secret`, `auth`
- Connection strings: `mysql://`, `postgres://`, database URLs
- Credit cards: Card number patterns
- Email addresses: (optional, disabled by default)

### Configuration

```yaml
global:
  log_redaction_enabled: true
  log_redaction_patterns:
    - "password"
    - "api_key"
    - "secret"
    - "token"
    - "authorization"
  log_redaction_placeholder: "***REDACTED***"
```

### Examples

**Before redaction:**
```json
{
  "msg": "Database connected",
  "connection": "mysql://user:secret123@localhost/db",
  "api_key": "sk_live_abc123def456"
}
```

**After redaction:**
```json
{
  "msg": "Database connected",
  "connection": "mysql://user:***REDACTED***@localhost/db",
  "api_key": "***REDACTED***"
}
```

**Laravel Log:**
```
Before: User login successful: password=secret123, token=abc123
After:  User login successful: password=***REDACTED***, token=***REDACTED***
```

### Compliance Support

**GDPR - PII Redaction:**
```yaml
log_redaction_patterns:
  - "email"
  - "phone"
  - "ssn"
  - "credit_card"
```

**PCI DSS - Credit Card Masking:**
```yaml
log_redaction_patterns:
  - "\\d{4}[\\s-]?\\d{4}[\\s-]?\\d{4}[\\s-]?\\d{4}"  # Card numbers
  - "cvv"
  - "card_number"
```

**HIPAA - PHI Protection:**
```yaml
log_redaction_patterns:
  - "patient_id"
  - "medical_record"
  - "diagnosis"
  - "ssn"
```

## Log Filtering

### Filter by Level

```yaml
global:
  log_filter_enabled: true
  log_filter_level: "warn"  # Only WARN and above (WARN, ERROR, FATAL)
```

**Result:** DEBUG and INFO logs are discarded.

### Filter by Pattern

```yaml
global:
  log_filter_enabled: true
  log_filter_patterns:
    - "health_check"     # Exclude health check logs
    - "metrics_export"   # Exclude metrics logs
    - "GET /health"      # Exclude health endpoint access
```

**Use cases:**
- Reduce noise from health checks
- Exclude high-frequency events
- Filter out known non-issues

### Per-Process Filtering

```yaml
processes:
  nginx:
    logging:
      filter_patterns:
        - "GET /health"  # Exclude health checks for nginx only
```

## Process Log Segmentation

### Per-Process Labels

```yaml
processes:
  php-fpm:
    logging:
      labels:
        service: php-fpm
        tier: backend
        app: my-php-app

  nginx:
    logging:
      labels:
        service: nginx
        tier: frontend
```

**Output:**
```json
{
  "time": "2024-11-21T10:00:00Z",
  "process": "php-fpm",
  "labels": {
    "service": "php-fpm",
    "tier": "backend",
    "app": "laravel"
  },
  "msg": "Request processed"
}
```

### Filter by Labels

```bash
# View only backend logs
docker logs app | jq 'select(.labels.tier=="backend")'

# View only nginx logs
docker logs app | jq 'select(.process=="nginx")'

# View by service
docker logs app | jq 'select(.labels.service=="php-fpm")'
```

## Complete Example

```yaml
version: "1.0"

global:
  log_format: json
  log_level: info

  # Multiline handling
  log_multiline_enabled: true
  log_multiline_pattern: '^\\[|^\\d{4}-|^{"'
  log_multiline_timeout: 500
  log_multiline_max_lines: 100

  # Redaction
  log_redaction_enabled: true
  log_redaction_patterns:
    - "password"
    - "token"
    - "api_key"
    - "secret"
  log_redaction_placeholder: "***REDACTED***"

  # Filtering
  log_filter_enabled: true
  log_filter_level: "info"
  log_filter_patterns:
    - "health_check"
    - "GET /health"

processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    logging:
      stdout: true
      stderr: true
      labels:
        service: php-fpm
        tier: backend

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    logging:
      stdout: true
      stderr: true
      filter_patterns:
        - "GET /health"  # Nginx-specific filter
      labels:
        service: nginx
        tier: frontend
```

## Log Aggregation

### Loki Integration

```yaml
services:
  app:
    logging:
      driver: loki
      options:
        loki-url: "http://loki:3100/loki/api/v1/push"
        loki-labels: "app=my-php-app"
```

**Query in Grafana:**
```logql
{app="my-php-app"} | json | line_format "{{.level}} [{{.process}}] {{.msg}}"
```

### Elasticsearch Integration

```yaml
services:
  app:
    logging:
      driver: fluentd
      options:
        fluentd-address: "localhost:24224"
        tag: "php-app.{{.Name}}"
```

### CloudWatch Integration

```yaml
services:
  app:
    logging:
      driver: awslogs
      options:
        awslogs-region: us-east-1
        awslogs-group: /ecs/php-app
        awslogs-stream: cbox-init
```

## Troubleshooting

### Stack Traces Split Across Logs

**Problem:** Multiline logs not being combined

**Solution:** Adjust pattern
```yaml
global:
  log_multiline_pattern: '^\\[|^[A-Z]{4,}:|^\\d{4}-'
  # Matches:
  # - [ERROR]
  # - ERROR:
  # - 2024-11-21
```

### Sensitive Data Still Visible

**Problem:** Credentials appearing in logs

**Solution:** Add pattern
```yaml
global:
  log_redaction_patterns:
    - "custom_secret_field"
    - "internal_token"
    - "mysql://.*:.*@"  # Connection strings
```

**Test redaction:**
```bash
echo 'password=secret123' | docker exec -i app /usr/local/bin/cbox-init --test-redaction
```

### Logs Too Verbose

**Solution:** Increase filter level
```yaml
global:
  log_filter_level: "warn"  # Was info
```

**Or exclude patterns:**
```yaml
global:
  log_filter_patterns:
    - "Debugbar"
    - "GET /horizon/api"
    - "metrics_collection"
```

## See Also

- [Global Settings](../configuration/global-settings) - Logging configuration
- [Process Configuration](../configuration/processes) - Per-process logging
- [Prometheus Metrics](../observability/metrics) - Structured monitoring

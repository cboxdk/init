---
title: "Advanced Logging"
description: "Per-process log level detection, multiline handling, JSON parsing, filtering, and sensitive-data redaction"
weight: 26
---

# Advanced Logging

Cbox Init can process each process's log stream: detect levels, reassemble
multiline output, parse JSON, filter noise, and redact sensitive data.

**All of these are configured per process under `processes.<name>.logging`.**
There is no global logging pipeline configuration — the settings below live on
each process, not under `global:`.

## Features

- ✅ **Log level detection:** Parse levels from log content
- ✅ **Multiline handling:** Reassemble stack traces into one entry
- ✅ **JSON parsing:** Extract structured fields from JSON logs
- ✅ **Redaction:** Mask sensitive substrings with regex rules
- ✅ **Filtering:** Include/exclude lines and set a minimum level
- ✅ **Per-process labels:** Tag logs by process

## The `logging` block

```yaml
processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    logging:
      stdout: true
      stderr: true
      min_level: info          # debug | info | warn | error (default: info)
      labels:
        service: php-fpm
        tier: backend
      redaction:      { ... }   # see below
      multiline:      { ... }
      json:           { ... }
      level_detection:{ ... }
      filters:        { ... }
```

## Log Level Detection

Detect a level from the log line content and attach it to the structured entry.

```yaml
logging:
  level_detection:
    enabled: true
    patterns:                       # map of level -> regex
      error: '(?i)\berror\b|\bfatal\b'
      warn:  '(?i)\bwarn(ing)?\b'
      info:  '(?i)\binfo\b'
    default_level: info             # used when nothing matches (default: info)
```

## Multiline Log Handling

Stack traces and multi-line errors otherwise arrive as separate lines. A
multiline buffer joins continuation lines into one entry.

```yaml
logging:
  multiline:
    enabled: true
    pattern: '^\[|^\d{4}-|^\{'   # regex marking the START of a new entry
    max_lines: 100               # max lines to buffer (default: 100)
    timeout: 1                   # flush timeout in SECONDS (default: 1)
```

**How it works:**
1. A line matching `pattern` starts a new entry.
2. Lines that do not match are appended to the current entry.
3. The entry is flushed after `timeout` seconds or `max_lines` lines.

## JSON Log Parsing

Parse JSON log lines and lift their fields into the structured output.

```yaml
logging:
  json:
    enabled: true
    detect_auto: true       # only treat lines starting with "{" as JSON
    extract_level: true     # use the line's level field as the entry's level
    extract_message: true   # use the line's message field as the entry's message
    merge_fields: true      # add the remaining fields as attributes
    message_field: msg      # optional; default: "message", then "msg"
    level_field: severity   # optional; default: "level"
```

| Option | Default | Description |
|--------|---------|-------------|
| `message_field` | `message`, then `msg` | Key `extract_message` reads. Unset, the first of `message` and `msg` that holds a string is used — `msg` is what Go's `log/slog`, logrus, zap and pino write |
| `level_field` | `level` | Key `extract_level` reads |

A field that is extracted is not repeated as an attribute. When a line has no
string under the message key, the whole line becomes the message.

**Fields that clash with cbox-init's own.** Every entry cbox-init writes already
has `time`, `level`, `msg`, `process`, `instance_id` and `stream`. A merged field
with one of those names is kept with an `app_` prefix instead of being written
twice — the process's own timestamp becomes `app_time`. (If `app_time` is taken
too, the prefix is repeated.) Merged fields come out sorted by their original
name.

For example, with every option on, this line from `postgres_exporter`:

```json
{"time":"2026-09-24T10:15:02.123Z","level":"INFO","source":"tls_config.go:347","msg":"Listening on","address":"127.0.0.1:9187"}
```

is written by cbox-init (`log_format: json`) as:

```json
{"time":"2026-09-24T10:15:02.130Z","level":"INFO","msg":"Listening on","process":"postgres-exporter","instance_id":"postgres-exporter-0","stream":"stderr","address":"127.0.0.1:9187","source":"tls_config.go:347","app_time":"2026-09-24T10:15:02.123Z"}
```

Lines that are not JSON — logfmt, plain text — are passed through unchanged as
the message. `level_detection` can still pick a level out of them, for example
`warn: '\blevel=WARN\b'` for logfmt.

## Filtering

Drop noisy lines or keep only the ones you care about, and set a minimum level.

```yaml
logging:
  min_level: warn           # discard entries below this level
  filters:
    exclude:                # drop lines matching any of these patterns
      - "GET /health"
      - "metrics_export"
    include:                # if set, keep ONLY lines matching these patterns
      - "ERROR"
      - "CRITICAL"
```

`min_level` filters by the detected/parsed level; `filters.exclude` and
`filters.include` match against the raw line.

## Sensitive Data Redaction

Redaction replaces substrings matched by a regex with a replacement string. Each
rule has a `name` (for reference), a `pattern` (regex), and a `replacement`.

```yaml
logging:
  redaction:
    enabled: true
    patterns:
      - name: password
        pattern: 'password=\S+'
        replacement: 'password=***REDACTED***'
      - name: bearer-token
        pattern: 'Bearer\s+[A-Za-z0-9._-]+'
        replacement: 'Bearer ***REDACTED***'
      - name: connection-credentials
        pattern: '(mysql|postgres)://[^:]+:[^@]+@'
        replacement: '$1://***:***@'
```

**Before:**
```
User login: password=secret123, token=Bearer abc.def.ghi
```

**After:**
```
User login: password=***REDACTED***, token=Bearer ***REDACTED***
```

Redaction is a best-effort text-masking feature that helps keep credentials out
of your log stream. It is not a compliance certification — you are responsible
for verifying that your patterns cover everything your policies require.

## Log File Tailing

A process can also tail local log files, each with its own processing options:

```yaml
logging:
  files:
    laravel:
      path: /var/www/storage/logs/laravel.log
      multiline:
        enabled: true
        pattern: '^\['
      rotate:
        max_size: "50MB"
        max_files: 5
```

## Complete Example

```yaml
version: "1.0"

global:
  log_format: json
  log_level: info

processes:
  php-fpm:
    command: ["php-fpm", "-F", "-R"]
    logging:
      stdout: true
      stderr: true
      min_level: info
      labels:
        service: php-fpm
        tier: backend
      multiline:
        enabled: true
        pattern: '^\[|^\d{4}-|^\{'
        max_lines: 100
        timeout: 1
      redaction:
        enabled: true
        patterns:
          - name: password
            pattern: 'password=\S+'
            replacement: 'password=***'
          - name: api-key
            pattern: 'api_key=\S+'
            replacement: 'api_key=***'

  nginx:
    command: ["nginx", "-g", "daemon off;"]
    logging:
      stdout: true
      stderr: true
      labels:
        service: nginx
        tier: frontend
      filters:
        exclude:
          - "GET /health"   # drop health-check access lines
```

## See Also

- [Global Settings](../configuration/global-settings) - Global log format and level
- [Process Configuration](../configuration/processes) - Per-process logging
- [Prometheus Metrics](../observability/metrics) - Structured monitoring

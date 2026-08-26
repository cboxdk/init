# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **A "public" name no longer exempts a real secret.** The public-value
  exemption was checked first, so `RECAPTCHA_SITE_SECRET`, `SITE_PRIVATE_KEY`
  and `PUBLIC_SECRET_KEY` were returned in cleartext. An explicit secret word now
  always wins; the exemption only overrides the weak "ends in key" match.
- **A readiness-only health check no longer restarts the process.** `mode:
  readiness` is documented as gating dependents, but a failing probe still killed
  and restarted the service. It now only gates readiness; `liveness` and the
  default `both` still restart.
- **A failed restart no longer strands its dependents.** When a replacement
  instance could not be started, readiness was left re-armed but never resolved
  and the all-processes-dead sweep was not run, so dependents waited out the full
  dependency timeout and the container could idle with no workload. The failure
  now resolves readiness and triggers the sweep.
- **A dead long-running dependency is no longer reported ready.** A process with
  no health check answered "ready" immediately even when all its instances had
  exited; it now reports that readiness is impossible.
- **A reload that cannot stop a process aborts instead of duplicating it.** The
  stop failure was logged and the reload continued, replacing the supervisor
  entry — leaving the old process running with nothing managing it, alongside a
  new copy. The reload now refuses and leaves the running configuration in place.

### Fixed

- **Public keys are no longer masked as secrets.** `PUBLIC_KEY`,
  `STRIPE_PUBLISHABLE_KEY`, `RECAPTCHA_SITE_KEY` and frontend-exposed variables
  (`MIX_*`, `VITE_*`, `NEXT_PUBLIC_*`) are published by definition, so hiding
  them in the API only cost the operator information.

### Fixed

- **Erlang/RabbitMQ cluster cookies are redacted** (`RABBITMQ_ERLANG_COOKIE`),
  while ordinary cookie settings (`COOKIE_DOMAIN`, `SESSION_COOKIE_NAME`) stay
  readable.
- **A long-running process that has died stops advertising itself as ready.**
  Readiness was only re-armed when a process started or auto-restarted, so one
  that exited without a restart (`restart: never`, or an exhausted restart
  budget) kept its previous run's readiness — a dependent started later saw a
  dead service as ready. A oneshot is unaffected: for it, finishing IS the
  success condition.

### Fixed

- **More secret env vars are redacted.** The abbreviated forms (`DB_PASS`,
  `MYSQL_PWD`), separator-less compounds (`SECRETKEY`) and registry credentials
  (`DOCKER_AUTH_CONFIG`) were still returned in cleartext. The short forms
  require a boundary on both sides, so `COMPASS_DIR` and `PASSENGER_ROOT` stay
  readable.
- **A restarted process is no longer treated as ready from its previous run.**
  Readiness was only re-armed on an explicit start, so a dependency that became
  ready, crashed, and was auto-restarted still looked ready — a dependent started
  afterwards (by a reload or scale-up) launched against a process that was only
  just booting. Automatic restarts now re-arm it too.
- **A readiness signal from a superseded run is ignored.** A waiter that
  sampled the signals just as the process restarted could observe the retired
  run's "ready" and release its dependents against the new, unproven one.
  Signals now carry a generation, and a waiter ignores any that is not from the
  current run.
- **The reload rollback counts processes it could not stop.** Stop failures were
  logged but not counted, so a rollback could report success while a process from
  the failed configuration was still running.

### Changed

- **BREAKING: REST log responses now use the same field names as the SSE
  stream.** `GET /api/v1/logs` and `GET /api/v1/processes/{name}/logs`
  serialized Go field names (`Timestamp`, `ProcessName`, `InstanceID`) while
  `/logs/stream` emitted `timestamp`, `process`, `instance` — two parsers for one
  concept. Both now emit the stream's spelling, from a single shared type, and
  the OpenAPI spec documents it. Consumers of the REST log endpoints must update
  their field names.
- **A oneshot can no longer be configured with `restart: on-failure`.** A
  oneshot's exit is handled by the completion path, which never consults the
  restart policy, so the setting silently did nothing — it is now rejected at
  config load with a pointer to `restart: never`.

### Fixed

- **More secrets are redacted, and a few settings are readable again.** The
  previous pass over the secret-name pattern fixed its false positives but
  introduced false negatives: `AWS_ACCESS_KEY_ID`, `PRIVATE_KEY_PEM`, WordPress's
  `*_SALT` values, separator-less names (`DBPASSWORD`) and camelCase
  (`jwtSecret`) were all returned in cleartext. Matching now requires a secret
  word to be followed by a non-letter (or end), which catches those while still
  leaving `TOKENIZER_PATH` and `SECRETARY_EMAIL` visible. Password-only URLs
  (`redis://:secret@host`, common for Redis and AMQP) are detected too.
- **Creating a process rejects the redaction placeholder.** `POST /processes`
  had no equivalent of the update path's guard, so cloning a process read from
  the API started it with `***REDACTED***` as its password — which
  `config/save` would then write to the YAML. It is now a 400.
- **The reload rollback budgets the whole rollback, waits for dependencies, and
  counts accurately.** It used a per-process timeout for the entire sequence, so
  it could report processes as down while their start was still in flight; it
  restarted processes without honoring `depends_on`, so a rolled-back stack could
  start a service before its migration finished; and its error path over-counted
  failures.
- **A waiter is no longer stranded when a process restarts underneath it.** If a
  supervisor re-armed its readiness signals while a dependent was already
  waiting, the waiter kept watching the retired channel until its timeout. It is
  now woken to watch the new run.
- **The API client escapes process names in URLs** (paths and the log-stream
  query), so a name containing a space or `&` no longer misroutes.
- **A completed oneshot releases its resource-sampling handle** instead of
  keeping a handle to a dead PID for the container's lifetime; its metrics
  history is retained.

### Fixed

- **Readiness is re-armed on every run.** The readiness signals were sticky for
  a supervisor's whole lifetime, so a restarted service counted as ready before
  it had proven anything, and — with readiness now also carrying a failure
  signal — a oneshot that failed once would keep failing its dependents forever,
  even after a successful re-run (both signals closed, the waiter picking between
  them at random). A new run now resets readiness, and the signals moved to their
  own mutex so a waiter cannot race the re-arm. (CONC-16 / PID1-7)
### Security

- **The process-detail API no longer leaks — or destroys — secrets.** Two
  problems with the env redaction: (1) a client that read a process (secrets
  masked), changed one field and PUT the whole config back wrote the literal
  `***REDACTED***` over the real secret and restarted the service with a broken
  environment — the TUI's edit flow did exactly this; the API now treats the
  placeholder as "keep the configured value". (2) The secret-name pattern both
  over- and under-matched: it masked ordinary settings (`AUTH_DRIVER`,
  `OAUTH_ENABLED`, `TOKENIZER_PATH`) while missing the ones that matter most for
  the frameworks this targets — Laravel's `APP_KEY`, and credential-bearing
  `DATABASE_URL`/`MAIL_DSN` values. Generic words now need word boundaries, the
  ambiguous ones (`auth`, `key`, `pat`) only count at the end of a name, and
  URL/DSN variables are masked when their value actually carries credentials.

### Fixed

- **A failed oneshot no longer stalls startup for the dependency timeout.** With
  oneshot readiness gated on successful completion, a oneshot that *failed* never
  signalled anything, so its dependents waited out the full `dependency_timeout`
  (5 minutes by default) — with the manager's write lock held, during which PID 1
  also could not act on SIGTERM. A failed oneshot now immediately signals that
  readiness is impossible, and dependents fail in milliseconds with an error
  naming the exit code.
- **A failed reload's rollback no longer runs on a dead context.** The rollback
  reused the reload's context, but the failures that trigger a rollback (a
  dependency wait timing out, a cancelled request) are exactly the ones that
  exhaust it — so the rollback force-killed the old processes and then refused to
  start any of them, turning a failed reload into an outage while reporting
  success. It now runs on a detached, separately-bounded context, tears down in
  reverse dependency order, and reports how many processes it could not restore
  instead of always claiming a clean rollback.
- **Per-process CPU% now really is recent usage.** The previous change cached the
  gopsutil handle but still called `CPUPercent()`, which divides total CPU time
  by the process's *lifetime* — so the gauge kept reporting a historical average
  no matter how the handle was managed (measured: an idle process still reported
  72.8%). The collector now uses the interval-based reading that the cached
  handle actually enables, so an idle process reads ~0%. The first sample after a
  process starts or restarts reports 0; the scale is unchanged (100 = one core).
- **`POST /processes` returns the right status code.** It hardcoded 500, so a
  duplicate name returned 500 instead of 409 and an invalid definition 500
  instead of 400, defeating the typed errors added earlier.
- **TUI: the detail view's footer advertised a dead key.** It still said
  `<s> Stop` after Stop moved to `x`, so pressing `s` there did nothing.
- **The log-stream endpoint's 405 is JSON like every other endpoint** (it was
  `text/plain`), and the API client no longer treats an empty 2xx body as a
  decode error.
- **The OpenAPI spec's `PUT /processes/{name}` body was wrong** — it declared a
  bare process object, but the endpoint requires a `{"process": {…}}` wrapper, so
  a generated client got a 400.

## [3.0.0] - 2026-08-26

### Added

- **An OpenAPI 3.0 spec for the management API.** Every endpoint — process
  lifecycle, scaling, per-process signal, logs (incl. the SSE stream), schedule
  control, config save/reload, metrics and oneshot history — is now described in
  `docs/observability/openapi.yaml`, with auth, parameters, request bodies and
  status codes. Load it into Swagger UI/Postman or generate a client from it.
  (DX-6)

- **Benchmarks for the hot paths.** The repo had a `make bench` target but no
  benchmarks, so there was no baseline to catch a performance regression.
  Added benchmarks for `TimeSeriesBuffer.GetRange`/`Add` (the resource-history
  read/write — `GetRange` now shows a single allocation, locking in the O(n)
  fix) and `ProcessWriter.Write` (the per-child log path, incl. cross-write
  partial-line assembly). (PERF-9)

- **Per-process signal action.** `cbox-init signal <process> <signal>` (and
  `POST /api/v1/processes/{name}/signal` with `{"signal":"SIGHUP"}`) delivers an
  operational signal to a single service's process group — an nginx config
  reload (`SIGHUP`), php-fpm log reopen (`SIGUSR1`) or graceful reload
  (`SIGUSR2`) — without touching the rest of the stack. The signal accepts the
  `SIGHUP` or bare `HUP` spelling; an unknown name is rejected with 400.
- **cbox-init now behaves like an init on the signal plane.** Previously only
  SIGTERM/SIGINT/SIGQUIT were handled (all as shutdown) and every other signal
  was dropped, so `docker kill -s HUP` was a silent no-op. Now:
  - **SIGHUP** reloads the configuration (works with or without `--watch`).
  - **SIGUSR1 / SIGUSR2** are forwarded to every managed process group, so
    operators can drive nginx reloads and php-fpm log reopen / graceful reload
    with `docker kill -s USR1|USR2 <container>`.
  - When cbox-init is **not PID 1** (a `docker run --init` wrapper, a shell
    entrypoint, or a Kubernetes pod sharing the PID namespace with the pause
    container), it now registers as a **child subreaper**
    (`PR_SET_CHILD_SUBREAPER` on Linux) so orphaned grandchildren still
    re-parent onto it and its zombie-reaping and restart guarantees keep
    applying. The startup log states which mode it is running in.

- Global lifecycle hooks can be defined entirely via environment variables —
  `CBOX_INIT_HOOK_<TYPE>_<N>_<FIELD>` (e.g. `CBOX_INIT_HOOK_PRE_START_0_COMMAND=php,please,stache:warm`)
  — so docker-compose/k8s deployments on prepared base images need no YAML
  mount for warmup or migration hooks. Env-defined hooks append after
  YAML-defined ones, ordered by index; `ALLOW_FAILURE` is accepted as the env
  spelling of `continue_on_error`. (#33)
- `COMMAND` env overrides (hooks and processes) accept a comma-separated list
  alongside the existing JSON-array form.
- Hooks are validated at config load (command required, non-negative
  timeout/retry/retry_delay) and unnamed hooks get a stable default name
  (`pre-start-0`, …) so logs and metrics always carry a hook identity.
- Hook execution logs carry structured `type`, `duration_seconds` and
  `exit_code` fields.

- The supply-chain gate the sibling packages run: `govulncheck`, a deterministic
  CycloneDX 1.5 SBOM (`tools/sbomnorm`) and a dependency license check
  (`tools/licensecheck`), plus gofmt and `go mod tidy` drift checks. CI had none
  of these, so nothing would have reported a vulnerable dependency or a
  non-permissive licence. 63 dependencies, all permissive.
- `make check` runs the whole gate locally, identical to CI.
- `bodyclose`, `errorlint` and `misspell`, added one at a time per this repo's
  own linter policy after fixing everything they surfaced.

- A documentation config drift gate: `tools/check-doc-configs.sh` (and
  `make check-configs`, wired into CI as the "Config examples" job) validates
  every `configs/examples/*.yaml` with `check-config`. Those files back the
  examples throughout `docs/`, so a config that references a removed or
  misspelled key now fails CI instead of silently misleading a reader.

### Changed

- **Shared, typed DTOs for the API's data responses.** The server built the
  process-list, process-detail, logs, and oneshot-history responses as ad-hoc
  `map[string]any`, and the client decoded each into a separate anonymous struct
  — no compile-time link between the two, so a field rename could silently break
  the client. These four shapes now live once in a new `internal/apitypes`
  package that both the server and the client import. Wire format unchanged.
  (STYLE-3, STYLE-5)

- **The API client is now SDK-quality.** `internal/apiclient` hand-rolled the
  same request/response/error block in ~15 methods, with inconsistent error
  messages that dumped raw JSON into CLI output. All methods now go through a
  single `do()` helper, and a non-2xx response surfaces as a typed `*APIError`
  carrying the HTTP status and the server's `error` message (matchable with
  `errors.As`) instead of a raw body. Behavior is unchanged; the file shrank from
  612 to 364 lines. (DX-7)

- **A oneshot now gates its dependents on completion, not on fork.** A oneshot
  process with no health check was marked "ready" the instant it started, so a
  process with `depends_on: [migrate]` began before the migration finished —
  breaking the canonical migrate-then-serve ordering. A oneshot is now ready only
  when it exits 0; its dependents wait for it (bounded by `dependency_timeout`),
  and if it fails they do not start. Long-running processes with no health check
  are unaffected (ready as soon as they run). This is a behavior change for
  configs that depend on a oneshot. (PID1-9)

- **The coverage claim is now enforced.** CLAUDE.md states the project targets
  >80% coverage, but nothing checked it and the Codecov upload had no
  configuration or threshold. `make check` (and CI) now runs a `cover-check`
  step that fails if total statement coverage drops below a floor
  (`COVERAGE_FLOOR`, currently 78% — a regression backstop below the 80%+ goal),
  and a `codecov.yml` sets the project/patch targets. (TEST-9)

- **Lifecycle hook types are now constants, and the four execution blocks share
  one helper.** The manager ran pre-start / post-start / pre-stop / post-stop
  hooks through four near-identical copy-pasted loops, each passing a bare string
  (`"pre_start"`, …) that becomes a Prometheus label — a typo would silently fork
  the metric series. Hook types are now `hooks.Type` constants used everywhere
  (manager and the per-process pre-stop hook), and a single `runHooks` helper
  handles all four lists with the correct fatal/non-fatal semantics. No behavior
  change. (STYLE-7)

- **`schedule_max_concurrent` is documented honestly and warns when misused.** The
  field advertised "> 1 allows parallel runs", but scheduled executions are always
  serialized (an overlapping run is skipped), so a value above 1 never had any
  effect. The comments now say so, and `check-config` emits a suggestion when a
  process sets `schedule_max_concurrent > 1`. The field is retained (existing
  configs keep loading) for a future parallel mode. (CONC-15)
- **The manager start-time metric is emitted from `Start()`, not the constructor.**
  `NewManager` wrote a global Prometheus gauge as a construction side effect, so
  merely building a manager for a dry-run or config check mutated global metric
  state. The write now happens when the manager actually starts. (ARCH-9)
- **Internal cleanup: removed dead code and duplication (no behavior change).**
  Deleted `cmd/helpers.go` and its tests — ~170 lines of exported helpers with
  zero production callers (a `package main`, so nothing could import them), whose
  `ResolveConfigPath` also disagreed with the config-path resolver `serve.go`
  actually uses. Replaced two identical package-local `contains` helpers
  (`internal/config`, `internal/deps`) with the standard library's
  `slices.Contains`, and swept the remaining `interface{}` type literals to the
  modern `any` spelling across the tree. (ARCH-8, STYLE-9)

- **BREAKING: the management API now binds loopback by default.** `api_host`
  defaulted to empty, meaning all interfaces — enabling the API with no
  `api_auth` and no `api_acl` served the full control plane (add/stop processes,
  save/reload config) unauthenticated on `0.0.0.0:9180`. `api_host` now defaults
  to `127.0.0.1`, and configuration validation **refuses to start** an API bound
  to a non-loopback interface without either a bearer token (`api_auth`) or an IP
  ACL (`api_acl`). To expose the API deliberately, set an explicit `api_host`
  (e.g. `0.0.0.0`) *and* configure auth or an ACL. The local Unix socket and
  loopback binds are unaffected; `metrics_host` is unchanged (Prometheus scrapes
  it on the pod/container IP).

- Moved `docker-compose.yaml` and `kubernetes-deployment.yaml` out of
  `configs/examples/` into a new `deploy/` directory. They are deployment
  manifests, not cbox-init configs, so keeping them in the examples glob would
  have made the drift gate above either wrong or littered with special cases.

### Fixed

- **TUI restart/stop now actually ask for confirmation, and Stop is `x`
  everywhere.** The help promised "(with confirmation)" for restart, start, and
  stop, but restart and stop fired immediately with no prompt. Worse, `s` meant
  Start in the process list but Stop in the detail view — so the same key started
  a service in one view and stopped it in another. Restart and stop now open the
  confirmation dialog in both views, Stop is bound to `x` consistently (Start
  stays `s`, and runs immediately since it is not destructive), and the help text
  matches the real behavior. (DX-4)

- **The live SSE log pipeline is now tested end to end.** `handleLogStream`,
  `SubscribeLogs`, and the log broadcaster had almost no coverage — the SSE
  endpoint had never served a request in a test. Added a test that connects to
  the stream, broadcasts a log entry through the manager, and asserts it arrives
  as an SSE data frame. (TEST-6)

- **Clearer CLI errors when the daemon is not reachable, and a `--url` alias on
  `tui`.** A control command that could not reach the API printed only a raw
  `dial tcp … connection refused`. All the client commands now share one error
  renderer that appends a hint ("The daemon may not be running. Start it with
  'cbox-init serve', or point --url …") on a connection failure. `tui` also
  accepts `--url` as an alias for `--remote`, so the endpoint flag is spelled the
  same everywhere. (DX-8)

- **The CLI control commands are now actually tested.** The `list`/`restart`/…
  subprocess tests asserted nothing (only `t.Logf`), so any regression in the
  client commands passed green. Added tests that run the CLI against an httptest
  server and assert on real output and exit codes: `list` prints the process rows
  and exits non-zero when a process is unhealthy; `restart` confirms on success
  and exits non-zero (with an error message) on a 404. (TEST-4)

- **Fixed a data race in resource sampling introduced by the CPU% handle
  cache.** Caching the gopsutil process handle (PERF-3) meant a handle could be
  read by two goroutines at once, and gopsutil's `Process` caches internal state
  and is not concurrency-safe. Each cached handle now carries a mutex that
  serializes samples taken through it (held outside the collector lock so one
  instance's sampling does not block others). Caught by the `-race` gate.

- **Per-process CPU% is now the recent usage, not a lifetime average.** The
  resource sampler created a fresh gopsutil handle every tick, and gopsutil
  computes `CPUPercent` as busy-time since the handle was created — so the
  `cbox_init_process_cpu_percent` gauge reported each process's lifetime-average
  CPU and sat nearly flat instead of tracking load. The collector now reuses the
  handle across ticks (recreating it if the instance restarts with a new PID and
  evicting it when the instance stops), so the gauge reflects usage since the
  previous sample. (PERF-3)

- **A failed reload no longer leaves services down — it rolls back.** Hot reload
  stopped the removed/changed processes and swapped the config *before* the new
  configuration was proven to start. If a new or changed process then failed to
  start (e.g. a bad command), the reload returned an error with services left
  stopped. The manager now snapshots the running configuration, and on a start
  failure tears down whatever the failed reload brought up and restores the
  previously-running processes on their old definitions (best-effort), returning
  an error that says it rolled back. (CDX-10)

- **405 responses now include an `Allow` header, and a doubled word in
  `check-config` is gone.** Every `405 Method Not Allowed` from the management
  API now advertises the accepted methods as RFC 7231 §6.5.5 requires (clients
  and proxies rely on it). And the suggestion formatter printed "→ Consider:
  Consider …" because the suggestions already begin with "Consider"; the
  redundant prefix is removed. (DX-13)

- **Stopping the management API is now bounded and never hangs.** `Server.Stop`
  called `http.Server.Shutdown(ctx)` and surfaced any error, so a lingering
  keep-alive connection made it block until the caller's deadline and then report
  "context deadline exceeded" (which also made the API stop tests flake on loaded
  CI runners). Stop now forces the connections closed if the graceful drain does
  not finish within the context, so it always returns promptly and cleanly.
  (TEST-8)

- **Startup no longer mixes log formats or claims the config is valid before it
  loads.** The permission-setup and runtime-validation phases ran before the
  logger was configured, so they logged in slog's text format while everything
  after config load was JSON — one startup, two formats. A structured logger is
  now bootstrapped from the environment (`LOG_FORMAT`/`LOG_LEVEL`, or the
  `CBOX_INIT_GLOBAL_*` overrides) before those phases, then refined from the
  config. The runtime php-fpm/nginx validation also logged a generic "All
  configurations valid", which read as if the cbox-init config had passed right
  before a "Failed to load configuration"; it now names the runtime service
  configs explicitly. (DX-9)

- **The Laravel scaffold no longer generates a queue worker that races Horizon.**
  `scaffold laravel` enabled both Horizon and a raw `queue:work --queue=default`
  worker by default. Horizon supervises its own workers for every queue, so the
  two drained the `default` queue simultaneously and raced for each job. The raw
  worker is now omitted whenever Horizon is enabled (Symfony and Horizon-less
  Laravel setups still get it). The generated API block also documents its
  loopback-by-default binding and how to secure it. (DX-11)

- **The quick-start guide now actually builds and runs.** Its Dockerfile
  `COPY nginx.conf` referenced a file the guide never created, so `docker build`
  failed on the first step, and the Nginx `/health` check pointed at a route no
  config served. The guide now includes a minimal working `nginx.conf` (health
  endpoint plus a PHP-FPM `fastcgi_pass`), points readers at `scaffold` and
  `check-config`, drops a reference to a non-existent `priority` field, and notes
  the loopback-by-default API binding. The `api_host` field comment in the config
  schema, which still described the old all-interfaces default, is corrected to
  match. (DX-12)

- **Hot reload no longer silently ignores changes to `user`, `group`, logging,
  memory limits, heartbeat, or scaled-port settings.** `Process.Equal` — which
  the config reload uses to decide whether a process needs restarting — compared
  only a subset of fields, so editing a service's `user:` (e.g. dropping it from
  root to an unprivileged account) and issuing a reload left it running under the
  old identity with no indication. `Equal` now covers every field of the process
  definition, including the full `logging`/`heartbeat` trees, and a drift-guard
  test asserts each field is compared. (CDX-9)

- **A slow service can no longer hold shutdown past the global deadline.** When
  stopping an instance, the graceful wait was bounded only by that process's own
  `shutdown.timeout`, ignoring the caller's context — so a service with, say,
  `shutdown.timeout: 300` kept PID 1 (and, under the manager lock, the API)
  waiting five minutes even when the global `shutdown_timeout` was 30s. The wait
  now also honors the context deadline the manager derives from
  `shutdown_timeout`, escalating to the kill signal as soon as either fires.
  (PID1-5)
- **A panic in the restored-process watcher no longer crashes PID 1.**
  `monitorRestored` — the goroutine that watches a warm-tier process brought back
  from a checkpoint — had no panic recovery, unlike its sibling `monitorInstance`,
  so a panic in the restart path would take down the init process. It now recovers,
  marks the instance failed, and always closes its done channel. (CONC-12)

- **The container exits non-zero when its workload dies.** When all managed
  processes were dead, PID 1 returned 0, so Docker `restart: on-failure` and
  Kubernetes saw a *successful* exit and did not restart a crash-looped
  container. cbox-init now exits non-zero when the death is abnormal — any
  process exited non-zero or was signalled, or a long-running process is dead
  even after a clean exit — while a container whose only work was a oneshot that
  completed with exit 0 still exits 0.
- **Lifecycle hooks, exec health checks, and scheduled jobs no longer lose their
  exit status to the PID-1 reaper.** As PID 1, cbox-init runs a wildcard reaper
  (`Wait4(-1)`) that races the `Wait()` of any child it spawns directly. Only
  supervised processes registered with it; hooks, exec health checks, and
  scheduled jobs called `Run()`/`CombinedOutput()` unregistered, so if the reaper
  won the race their `Wait()` returned `ECHILD` and the run was misread as a
  failure — a *successful* pre-start hook could abort container startup, and a
  *passing* exec health check could trigger a restart. All three now run through
  a shared `signals.RunSupervised` that registers the child before waiting and
  recovers the reaper-captured status (a clean exit stays a success). The common,
  non-raced path is unchanged.
- **Processes added or updated via the API are now fully validated.** Only the
  command and scale were checked, so a `POST /processes` with a typo'd restart
  policy (`on_failure`) or an invalid `type`/`initial_state` was accepted with
  201 and silently degraded — an unknown restart policy became "never", so the
  service crashed once and stayed down. The manager now applies defaults and
  validates the whole definition (rejected with 400).
- **A checkpointed process is no longer counted as dead.** The all-processes-dead
  sweep treated a warm-tier checkpointed instance as not-running, so a container
  whose workload was snapshotted could trip the "all dead → shut down" path and
  discard the checkpoint. Checkpointed instances now count as alive.
- **Fixed a data race on the process scale config.** `scaleFromZero` wrote the
  process's `Scale` without the manager lock, racing config reads and the locked
  scale updater; it now routes through the locked path.
- **API status codes are now correct and typed, not guessed from the message.**
  `httpStatusFromError` matched on message substrings, so rewording a manager
  error silently flipped a 404 to a 500, an unrelated `exec: … executable file
  not found in $PATH` was misreported as 404, and a wrong-state operation (e.g.
  starting an already-running process) returned 500 instead of a 4xx. The manager
  and scheduler now return typed sentinel errors (`ErrProcessNotFound`,
  `ErrProcessExists`, `ErrInvalidState`, `ErrInvalidArgument`, `ErrJobNotFound`),
  and the API maps them with `errors.Is` — not-found → 404, exists/wrong-state →
  409, bad-input → 400.
- **Shutdown no longer hangs behind a running scheduled job.** The manager
  waited unboundedly for the scheduler to stop while holding its write lock, and
  cron jobs ran under `context.Background()` so shutdown couldn't cancel them — a
  wedged job (a hung migration) froze shutdown and the API indefinitely. The
  scheduler now runs jobs under a lifetime context it cancels on stop, and the
  manager bounds the stop wait with the shutdown timeout.
- **Async job triggers now run to completion.** `POST /processes/{name}/schedule/trigger`
  ran the job under the HTTP request's context, which net/http cancels the moment
  the handler returns its `202` — so the job was cancelled right after being
  accepted. The async trigger now detaches from the request's cancellation.
- **Resource-metrics history is no longer O(n²) to read.** `TimeSeriesBuffer.GetRange`
  prepended each sample to a growing slice, reallocating and copying the whole
  result every iteration (~260k element copies for a full 720-sample read). It now
  appends newest-first and reverses once.
- **Fixed a data race in the API rate limiter.** A visitor's `lastSeen` was
  written on every request without the limiter lock while the cleanup goroutine
  read it under the lock. It is now an atomic value.
- **The API route table is defined once.** `Start` and `StartSocketOnly` each
  hand-maintained an identical list of routes differing only by the auth flag, so
  a new endpoint added to one and forgotten in the other would silently 404 in
  that mode. Both now build their mux from a single `buildMux` helper.
- **Misspelled config keys are now rejected instead of silently ignored.** A
  mistyped `health_chek:` used to turn off health checking with no warning, and a
  fabricated key like `scale_locked:` did nothing. Config loading now rejects any
  key that is not a real field, reporting the offending key with the file's own
  line number. (This surfaced a dead `scale_locked` key in the development
  example, now removed.)
- **`check-config` reports the same error every run.** The fail-fast validator
  iterated processes in map order, so a config with several problems surfaced a
  different error each run — whack-a-mole to fix and non-deterministic in CI. It
  now iterates in name order.

- **Health checks no longer kill-loop a slow-starting service.** After a
  health-triggered restart, the monitor kept its accumulated failure count and
  gave the replacement no warmup grace, so a service that took longer than one
  probe period to boot was killed on the very next check and abandoned once its
  restarts ran out. The monitor is now re-armed on each health restart — failure
  history reset and a fresh `initial_delay` grace window applied — so the
  replacement is judged from a clean slate.
- **The health-check monitor no longer leaks a goroutine on stop.** Its status
  sends were not guarded by the context, so if the consumer had already exited
  with a status buffered, the monitor blocked forever on the capacity-1 channel,
  leaking the goroutine and its ticker on every supervisor stop. The sends now
  select on the context.
- **Editing one process no longer restarts the whole stack.** `UpdateProcess`
  already stops and restarts the edited process with its new config, then
  additionally called an internal `restartAllProcesses` that bounced *every*
  running process (nginx, php-fpm, every sibling) in map order, ignoring the
  dependency DAG and restarting the just-edited process a second time. The
  redundant call is gone; a one-process edit now touches only that process.
- **Processes added or updated at runtime are now wired for death detection.**
  Supervisors created by `AddProcess`/`UpdateProcess` skipped
  `SetDeathNotifier`, so if such a process crashed with its restarts exhausted,
  the manager's "all processes dead → shut down" detection never heard about it
  and the container ran on as an empty shell. All four supervisor-construction
  sites now go through one helper, so the wiring can't drift.
- **Concurrent starts no longer multiply instances.** The manager checked a
  process's state before starting it, but outside any lock, so two callers
  (API + TUI + watcher) could both pass the check and each start `Scale`
  instances. `Supervisor.Start` is now idempotent for an already-running
  supervisor.
- **The log writer is now thread-safe, and partial lines are assembled
  correctly.** `ProcessWriter.Write` mutated an unsynchronized buffer, so the
  scheduled-job path — which hands the *same* writer to both `cmd.Stdout` and
  `cmd.Stderr`, which os/exec then drives with two concurrent copier goroutines —
  raced on it and could panic (fatal in PID 1). A mutex now guards the buffer and
  multiline state. Separately, `Write` used a `bufio.Scanner`, which emits the
  final token even without a trailing newline: a line flushed mid-write was split
  into several log entries, and the incomplete-line / oversized-buffer handling
  was unreachable dead code. `Write` now consumes only through the last newline
  and holds the trailing partial line until the next write or `Flush`.
- **A single log request could crash PID 1.** `GET /api/v1/logs?limit=N` and the
  per-process logs endpoint pre-allocated `len(processes) * limit` entries with
  no upper bound, so a large `limit` requested a multi-GB backing array (or
  overflowed to a negative capacity) — a runtime-fatal `make()` in PID 1,
  reachable on the unauthenticated local socket. The limit is now clamped to
  10000 (matching the metrics-history endpoint) and the manager caps its
  pre-allocation hint independently.

- **`shutdown.kill_signal` is now honored, and signal names are validated.**
  The per-process `shutdown.kill_signal` was defaulted to `SIGKILL` but read
  nowhere — `stopInstance` hard-coded `SIGKILL`, so a configured value did
  nothing. It is now used to force-kill an instance that overruns its graceful
  timeout. `parseSignal` recognized only four names and silently coerced
  everything else (including a valid `SIGUSR2`) to `SIGTERM`; it now understands
  the full set of forwardable/stop signals in both `SIGTERM` and bare `TERM`
  spellings, and `check-config` rejects an unknown `shutdown.signal` /
  `shutdown.kill_signal` instead of letting it silently degrade.
- **`check-config --json` now emits real JSON.** The flag advertised for CI/CD
  hand-rolled its serialization and printed Go syntax instead — a `[{ … map[errors:0 …] }]`
  dump that no `jq` pipeline or JSON parser could read, and the error path
  interpolated unescaped error text so a multi-line YAML error produced broken
  JSON too. Output now goes through `json.MarshalIndent`, round-trips through any
  JSON parser, and load/validation failures print a properly-escaped
  `{"error":"…"}`. The documented schema in `configuration/validation.md` was
  fictional (`valid`/`recommendation`/`counts`); it now matches the real output.
- **Graceful shutdown now actually happens.** Every managed process is launched
  with `exec.CommandContext`, and `Supervisor.Stop` cancelled that context
  *before* running the pre-stop hook and sending the configured shutdown signal.
  Go's os/exec watchdog SIGKILLs a child the instant its context is cancelled, so
  on every stop path — `docker stop`, `cbox-init stop`/`restart`, hot-reload,
  scale-to-zero — the child was force-killed before it ever saw SIGTERM. The
  configured `shutdown.signal`, pre-stop hooks, graceful timeout, and SIGKILL
  escalation were all dead code; PHP-FPM and nginx lost in-flight requests on
  every deploy. Context cancellation no longer kills children (`cmd.Cancel` is a
  no-op); `stopInstance` is the sole stop authority (pre-stop hook → configured
  signal → timeout → SIGKILL), and aborted-start cleanup force-kills survivors
  explicitly. Regression tests now assert a child actually *receives* SIGTERM and
  that a child ignoring it is force-killed after the timeout — the assertion no
  test made before, which is why this survived.

- **Documentation described several subsystems that do not exist.** Removed or
  corrected against the actual Go source (reviewer notes DOC-1/DOC-2, P0):
  - **Heartbeat monitoring** was documented as a full feature
    (`url`/`success_url`/`failure_url`/`method`/`headers`/`retry_count`, plus
    `cbox_init_heartbeat_*` metrics). None of it exists: `HeartbeatConfig`
    (`internal/config/types.go`) has only `enabled`/`interval`/`grace` and is
    read by no runtime code. The page is now an honest "planned / not yet
    implemented" stub, and the dead `heartbeat:` blocks were stripped from
    `configs/examples/scheduled-tasks.yaml`, `laravel-full.yaml`,
    `tui-test.yaml`, and the docs.
  - **Fabricated scheduled-task Prometheus metrics**
    (`cbox_init_scheduled_task_last_run_timestamp`/`next_run_timestamp`/
    `last_exit_code`/`duration_seconds`/`total`) and the alert rules built on
    them were removed — `internal/schedule` exports no metrics. The scheduler
    docs now point at the real status API
    (`GET /api/v1/processes/{name}/schedule` and `.../schedule/history`).
  - **Playwright / "Cbox Init Web UI" test suite** (`docs/development/testing.md`
    described 35 Playwright tests, a `web/` directory, WCAG 2.1, "100%
    coverage"). None of it exists; the page now documents the real Go test
    story (`make test`, race + coverage, Docker integration suite).
- **Documented configuration schemas did not match the code:**
  - Health-check docs used `interval`, `retries`, `expected_body`, and
    `address` for HTTP. The real `HealthCheck` struct uses `period` (10),
    `failure_threshold` (3), `initial_delay` (5), `url` for HTTP, and
    `mode: liveness|readiness|both`; there is no body matching. Fixed across
    the health-check, processes, container-readiness, scaffolding, dev-mode,
    validation, and feature-index docs.
  - The health metric was documented as `cbox_init_process_health_status`; the
    real name is `cbox_init_health_check_status{name,type}`
    (`internal/metrics/collector.go`).
  - Advanced-logging docs described a flat global schema
    (`log_multiline_enabled`, `log_redaction_patterns`, `log_filter_*`) that
    does not exist. Rewrote around the real per-process `logging:` block
    (`multiline` / `redaction` as `{name,pattern,replacement}` rules /
    `filters` / `min_level`), and dropped the GDPR/PCI/HIPAA compliance
    claims (redaction is a best-effort feature, not a certification).
- 26 files were not gofmt-clean; CI had no formatting gate to notice.
- `handleExecutionError` used a type assertion on the error, and two comparisons
  used `==`/`!=`, all of which stop matching once an error is wrapped.
- Stale PHPeek branding in the TUI header and keyboard-shortcut screen, the
  audit log's start and shutdown records, the build-info metric's help text and
  a configuration warning.

### Security

- **The process-detail API redacts secret-looking environment variables.**
  `GET /api/v1/processes/{name}` returned the process's full `env` in cleartext,
  so an authenticated operator (or anything with API access) saw values like
  `DB_PASSWORD`, `API_TOKEN`, or `AWS_SECRET_KEY`. Values whose variable name
  matches a secret pattern (password/secret/token/credential/api-key/access-key/
  private-key/auth) are now masked as `***REDACTED***` in the response;
  non-secret vars and the running configuration are unaffected. (SEC-4)

- **`trust_proxy` no longer lets any client spoof its source IP.** With
  `api_acl.trust_proxy` enabled, the `X-Forwarded-For` header was trusted from
  *any* direct connection, so a client bypassing the proxy could send
  `X-Forwarded-For: <allowed-ip>` and defeat the IP ACL. A new
  `api_acl.trusted_proxies` list names the reverse proxies whose header is
  honored; `X-Forwarded-For` is now used only when the direct peer is one of
  them, and is ignored otherwise (fail closed, including when the list is empty).
  `check-config` warns when `trust_proxy` is set without `trusted_proxies`. (CDX-7)

- **TLS `min_version` is now floored at 1.2.** A configured `min_version` of
  `"TLS 1.0"` or `"TLS 1.1"` was honored verbatim, silently serving the
  management API over protocol versions deprecated by RFC 8996 and vulnerable to
  known downgrade/padding attacks. Those values are now clamped up to TLS 1.2 and
  the operator is warned once at startup that the weaker floor did not take
  effect. Defaults and `"TLS 1.3"` are unchanged. (SEC-6)
- **`config/save` refuses to write a config derived from the environment.** The
  in-memory config holds `${VAR}` placeholders already resolved to their (often
  secret) values, plus any `CBOX_INIT_*` overrides that live only in the process
  environment. Saving it — via the API `POST /config/save` or the TUI — wrote
  those secrets to disk in cleartext and destroyed the templates. `SaveConfig`
  now detects both cases and refuses with an explanatory error, pointing the
  operator at the file instead. Configs with no templating or overrides save as
  before. (SEC-2)

- **The local management socket is now owner-only (`0600`).** It was `0660`, so
  any process in the socket's group could drive the full, unauthenticated
  control plane (add/stop processes, save/reload config). It is an admin channel
  and is now restricted to its owner.
- **The management API and process credentials now fail closed.** Two paths
  failed *open* — proceeding in a less-secure state after a configuration error:
  - An **invalid but enabled ACL** (e.g. a malformed CIDR) logged the error and
    then served the endpoint with **no ACL at all** — exposing exactly what the
    operator was trying to lock down. The server now refuses to start.
  - A **user/group that could not be resolved** (a typo in `user:`) logged the
    error and ran the process as PID 1's uid — i.e. **as root**. The process now
    refuses to start instead of silently dropping the privilege drop.

## [2.5.1] - 2026-08-11

### Fixed

- **Stopping a process that has already exited is no longer reported as a failure.**
  The signal races the child's own exit: on a loaded machine the process can go
  between the liveness check and the kill, `Process.Signal` returns
  `os.ErrProcessDone`, and the stop came back "failed to send signal". `scale 0`
  then reported an error for work that was already done. `os.ErrProcessDone` and
  `ESRCH` now mean what they say; `EPERM` still fails.
- **Dependencies swept to current** — gopsutil 4.26.7, client_golang 1.24.1,
  fsnotify 1.10.1, cobra 1.10.2, otel 1.45.0, bubbles 1.0.0, alpine 3.24, and
  every GitHub Action to its current major.
- **CI could not have caught either.** `syscall.Getsid` does not exist on Linux,
  so the test build failed on the only platform this ships on, and
  golangci-lint-action v6 refuses a v2 golangci-lint, so the lint job never ran.

## [2.5.0] - 2026-08-11

### Added

- **Snapshot: checkpoint and restore a running container's processes.** A coordinator
  quiesces supervised children, owns their stdio across the checkpoint so a restored
  process keeps writing to the same pipes, and treats a checkpointed exit as a
  checkpoint rather than a crash — no restart storm on resume. The warm tier survives
  repeated cycles and recovers from an interrupted one.
- **Engine-aware autotuning for Percona and Valkey.** Sizing is derived from the
  container's real memory and CPU limits and applied at startup, including InnoDB's
  buffer-pool rounding, which previously made the applied value disagree with the
  computed one.

### Fixed

- **Every Cbox base image shipped a binary with a CRITICAL CVE.** `cbox-init` linked
  grpc v1.75.0 (CVE-2026-33186, improper HTTP/2 handling; plus GHSA-hrxh-6v49-42gf)
  and was built with Go 1.24, whose stdlib carries eight more across `net/url`,
  `crypto/x509`, `crypto/tls` and `net/http`. Trivy had been reporting this on every
  image build, but the scan runs after the push, so it never stopped a publish.
  grpc 1.83.0, `x/net` 0.57.0, `x/text` 0.40.0, and the toolchain moves to Go 1.26 in
  go.mod, both workflows and the Dockerfile.
- **Go MPTCP is disabled so the process is checkpointable.** CRIU cannot parasite-inject
  through the MPTCP sockets Go dials by default, which made scale-to-zero
  suspend/resume fail on any container running cbox-init as PID 1.

## [2.4.1] - 2026-07-15

### Fixed
- Documentation restructured for findability and dead links fixed, including the PHP-FPM auto-tuning guide, which is now reachable from the environment-variables reference.

## [2.4.0] - 2026-07-10

### Fixed

- **PID-1 zombie reaper no longer races with the supervisor's own `Wait()`.** The wildcard
  reaper (`Wait4(-1)`) could reap a supervised child before its `cmd.Wait()` ran, leaving a nil
  `ProcessState` that panicked `monitorInstance` and left the process permanently in `failed`
  (never restarted, `/readyz` stale). Supervisors now register their PIDs; a racing reaper stashes
  the exit status for the supervisor to recover, so exit codes are preserved and restarts always run.
- **Restart budget is now a sliding window, not a lifetime total.** `restartCount` accumulated for
  the life of the container, so a service that crashed more than `max_restart_attempts` times — even
  with days of healthy uptime between crashes — was abandoned forever. A new
  `global.restart_stability_window` (default `60s`) resets the budget once an instance stays up long
  enough to be considered recovered (systemd `StartLimitIntervalSec` semantics; negative disables).
- **Log tailer no longer leaks a file descriptor on every rotation.** A `defer file.Close()` inside
  the reopen path accumulated one open FD per rotation, eventually exhausting `RLIMIT_NOFILE` on
  high-rotation logs.
- **Shutdown no longer skips processes when the dependency graph is invalid.** If a reload swapped in
  a config whose dependencies no longer resolve, `getShutdownOrder` returned an empty list and left
  children to be SIGKILLed by the runtime; it now falls back to stopping all known processes.

### Security

- **Rate limiter can no longer be bypassed via `X-Forwarded-For` spoofing.** It now keys on the
  real client IP (RemoteAddr), only honoring `X-Forwarded-For` when `trust_proxy` is enabled —
  previously a client could send a unique header per request to mint a fresh token bucket and defeat
  rate limiting (and brute-force the API token).
- Config files are now written `0600` (was `0644`). A saved config can contain the management-API
  bearer token (`api_auth`), so it must not be world-readable by other UIDs in the container.
- Recursive ownership fixes use `Lchown` instead of `Chown`, so a symlink planted in a user-writable
  mounted volume (storage/, wp-content/) can't redirect the chown at an arbitrary target file.

### Added

- **`api_host` / `metrics_host`** settings to restrict the API and metrics listeners to a specific
  interface (e.g. `127.0.0.1`). Default is unchanged (all interfaces), so this is opt-in hardening.

### Fixed (reload)

- **Config reload now validates the new config before stopping anything.** An invalid config (bad
  settings or a dependency cycle) previously stopped the removed/changed services first and only then
  failed, leaving them down. A failed reload is now a no-op — the running configuration is untouched.

### Changed

- Local `make build` now derives the version from `git describe` instead of a hardcoded `1.0.0`, so
  locally built binaries report the real version. `make dev` runs against `configs/examples/minimal.yaml`.
- Added `.golangci.yml` (v2), pinned `golangci-lint` in CI, `SECURITY.md`, `CONTRIBUTING.md`, and
  Dependabot for Go modules, GitHub Actions, and Docker.

## [2.3.1] - 2026-07-08

### Fixed

- **Supervised-process stdout/stderr now actually reaches container stdout.** Info-level output
  from tracked processes (php-fpm, nginx, queue workers, and JSON- or file-tailed lines detected
  as info) was routed through `slog.Debug()` in the process-log pipeline and therefore silently
  dropped by the default `log_level: info` handler — only `warn`/`error` survived. The result: a
  container's normal application logs never appeared in `kubectl logs` / `docker logs`, and a
  Laravel error logged via `LOG_CHANNEL=stderr` was invisible. Info-level process entries are now
  emitted at info, restoring the documented default that `logging.stdout` / `logging.stderr`
  forward process output. (The `default` switch arm was likewise corrected from debug to info.)

## [2.3.0] - 2026-07-07

### Added

- **Active HTTP readiness/liveness endpoints** — set `global.readiness.http_port` to expose:
  - `GET /readyz` → `200` when all tracked processes are ready, `503` otherwise (JSON body lists
    each tracked process's state). Drives the Kubernetes `readinessProbe`.
  - `GET /livez` → `200` whenever cbox-init can answer. Drives the `livenessProbe`.

  Unlike the readiness **file** (passive, can go stale if the supervisor wedges), these are served
  by cbox-init itself: a hung supervisor stops answering and the probe fails. The file remains for
  `exec` probes — the two are complementary. Bound to `0.0.0.0` by default so the kubelet can reach it.

## [2.2.0] - 2026-07-07

### Added

- **Startup performance controls** — new options to tune process manager startup behavior.

### Changed

- **Dependency-aware reload** — configuration reloads now respect process dependency ordering.
- **Secure runtime observability defaults** — metrics/observability endpoints now ship with safer, locked-down defaults.

### Fixed

- **`version` reported the wrong number on every release** — the version was a `const`, which the `-ldflags "-X main.version=..."` linker flag cannot override, so all builds reported `1.0.0`. It is now a `var` (default `dev`); release builds report the injected semver.
- **Health readiness semantics** — a process is now only reported ready when it is genuinely ready (fixes false-healthy reporting).
- **Hardened process lifecycle handling** — more robust start/stop/restart and supervisor edge-case handling.

## [2.1.1] - 2026-05-07

### Fixed

- **Permission setup: respect PUID/PGID and auto-detect www-data uid** — Framework directory ownership (`storage/`, `var/`, `wp-content/`) previously hardcoded uid/gid 82 (Alpine convention), which silently broke on Debian-based images where `www-data` is uid 33. The binary now resolves the app user via: (1) `PUID`/`PGID` env vars, (2) `/etc/passwd` lookup of `www-data`, (3) fallback to 82/82. This fixes Laravel 500 errors caused by view cache write failures on `php-fpm-nginx:*-bookworm` images.

## [2.1.0] - 2026-05-07

### Added

- CLI commands for process control (`list`, `status`, `start`, `stop`, `restart`, `scale`, `reload-config`, `logs`)
- Always-on Unix socket for CLI-to-daemon communication
- Log file tailing with rotation support
- API client package (`internal/apiclient`) extracted from TUI
- Log subscriber system for real-time log streaming

## [2.0.1] - 2026-04-17

### Fixed

- Oneshot processes now default to `restart: never` instead of inheriting the global restart policy

## [2.0.0] - 2026-04-17

### Changed

- Rebranded from phpeek-pm to cbox-init

## [1.2.2]

### Added

- Scaffolding `--observability` flag and streamlined presets

# Node.js Applications

Cbox Init is fully compatible with Node.js applications including Next.js, Nuxt, Remix, Express, Fastify, NestJS, and any other Node.js server.

## Quick Start

### Basic Configuration

```yaml
version: "1.0"

processes:
  app:
    enabled: true
    command: ["node", "dist/server.js"]
    restart: always
    env:
      NODE_ENV: production
      PORT: "3000"
    health_check:
      type: http
      url: http://localhost:3000/health
      initial_delay: 5
      period: 10
```

### Next.js (Standalone Mode)

```yaml
# Requires: output: 'standalone' in next.config.js
processes:
  nextjs:
    command: ["node", ".next/standalone/server.js"]
    restart: always
    env:
      NODE_ENV: production
      HOSTNAME: "0.0.0.0"
    port_base: 3000
    max_memory_mb: 512
    health_check:
      type: http
      url: http://localhost:3000/api/health
```

### Nuxt 3

```yaml
# Build output: .output/server/index.mjs
processes:
  nuxt:
    command: ["node", ".output/server/index.mjs"]
    restart: always
    env:
      NODE_ENV: production
      NITRO_HOST: "0.0.0.0"
    port_base: 3000
    max_memory_mb: 512
```

## Horizontal Scaling

### Port Management

When scaling Node.js apps (`scale: N`), each instance needs a unique port. Cbox Init provides two mechanisms:

#### 1. `port_base` (Recommended)

Automatically sets the `PORT` environment variable for each instance:

```yaml
processes:
  api:
    command: ["node", "server.js"]
    scale: 4
    port_base: 3000  # Instance ports: 3000, 3001, 3002, 3003
```

Your app should read the port from environment:

```javascript
const port = process.env.PORT || 3000;
app.listen(port);
```

#### 2. Instance Index Environment Variable

Cbox Init sets `CBOX_INIT_INSTANCE_INDEX` (0, 1, 2, ...) for each instance:

```javascript
const basePort = 3000;
const instanceIndex = parseInt(process.env.CBOX_INIT_INSTANCE_INDEX || '0');
const port = basePort + instanceIndex;
```

### Load Balancing with Nginx

```nginx
upstream nodejs {
    server 127.0.0.1:3000;
    server 127.0.0.1:3001;
    server 127.0.0.1:3002;
    server 127.0.0.1:3003;
}

server {
    listen 80;
    location / {
        proxy_pass http://nodejs;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
    }
}
```

## Memory Leak Protection

Node.js applications can suffer from memory leaks. Cbox Init can automatically restart processes that exceed a memory threshold:

```yaml
processes:
  api:
    command: ["node", "server.js"]
    max_memory_mb: 512  # Restart if RSS exceeds 512MB
    restart: always
```

The memory check runs alongside resource metrics collection (default: every 5 seconds). When exceeded, the process receives SIGTERM for graceful shutdown.

## Graceful Shutdown

Node.js apps must handle SIGTERM to shut down gracefully. Example:

```javascript
// server.js
const server = app.listen(port);

// Graceful shutdown handler
process.on('SIGTERM', () => {
  console.log('SIGTERM received, shutting down gracefully');

  server.close(() => {
    console.log('HTTP server closed');
    // Close database connections, etc.
    process.exit(0);
  });

  // Force exit after timeout
  setTimeout(() => {
    console.error('Forced shutdown after timeout');
    process.exit(1);
  }, 30000);
});
```

### Pre-Stop Hooks

For connection draining or cleanup:

```yaml
processes:
  api:
    command: ["node", "server.js"]
    shutdown:
      signal: SIGTERM
      timeout: 60
      pre_stop_hook:
        name: drain-connections
        command: ["node", "scripts/drain.js"]
        timeout: 10
```

## Health Checks

### HTTP Health Check

```yaml
health_check:
  type: http
  url: http://localhost:3000/health
  initial_delay: 5
  period: 10
  timeout: 3
  failure_threshold: 3
  expected_status: 200
```

Example endpoint:

```javascript
app.get('/health', (req, res) => {
  // Check dependencies
  const healthy = checkDatabase() && checkRedis();
  res.status(healthy ? 200 : 503).json({ status: healthy ? 'ok' : 'unhealthy' });
});
```

### TCP Health Check

For apps without HTTP endpoints:

```yaml
health_check:
  type: tcp
  address: 127.0.0.1:3000
  initial_delay: 3
  period: 10
```

### Exec Health Check

For custom health logic:

```yaml
health_check:
  type: exec
  command: ["node", "scripts/healthcheck.js"]
  timeout: 5
```

## Background Workers

### Queue Workers (BullMQ/Bull)

```yaml
processes:
  api:
    command: ["node", "dist/server.js"]
    scale: 2
    port_base: 3000

  worker:
    command: ["node", "dist/worker.js"]
    scale: 4
    restart: always
    depends_on: [api]
    max_memory_mb: 256
    env:
      REDIS_URL: redis://localhost:6379
      WORKER_CONCURRENCY: "10"
```

### Scheduled Tasks

```yaml
processes:
  scheduler:
    command: ["node", "scripts/cron.js"]
    schedule: "*/5 * * * *"  # Every 5 minutes
    restart: never  # Don't auto-restart scheduled tasks
```

## Logging

### JSON Log Parsing

Cbox Init can parse structured JSON logs (pino, winston, bunyan):

```yaml
processes:
  api:
    command: ["node", "server.js"]
    logging:
      stdout: true
      stderr: true
      json:
        enabled: true
        detect_auto: true
        extract_level: true
        extract_message: true
```

### Log Level Detection

For non-JSON logs:

```yaml
logging:
  level_detection:
    enabled: true
    patterns:
      error: "\\[ERROR\\]|error:|Error:"
      warn: "\\[WARN\\]|warn:|Warning:"
      debug: "\\[DEBUG\\]|debug:"
    default_level: info
```

## PM2 Migration Guide

| PM2 Feature | Cbox Init Equivalent |
|-------------|---------------------|
| `pm2 start app.js` | `command: ["node", "app.js"]` |
| `instances: 4` | `scale: 4` |
| `exec_mode: "cluster"` | `scale: N` + `port_base` |
| `max_memory_restart: "500M"` | `max_memory_mb: 500` |
| `watch: true` | `cbox-init serve --watch` |
| `env_production: {...}` | `env: {...}` |
| `cron_restart: "0 0 * * *"` | `schedule: "0 0 * * *"` |
| `wait_ready: true` | `health_check` with `mode: readiness` |
| `listen_timeout: 3000` | `health_check.initial_delay: 3` |
| `kill_timeout: 5000` | `shutdown.timeout: 5` |
| `pm2 logs` | `cbox-init tui` or API |
| `pm2 monit` | `cbox-init tui` |
| `pm2 reload` | API: `POST /processes/{name}/restart` |

### ecosystem.config.js to cbox-init.yaml

Before (PM2):
```javascript
module.exports = {
  apps: [{
    name: 'api',
    script: './dist/server.js',
    instances: 4,
    exec_mode: 'cluster',
    max_memory_restart: '500M',
    env_production: {
      NODE_ENV: 'production',
      PORT: 3000
    }
  }]
};
```

After (Cbox Init):
```yaml
processes:
  api:
    command: ["node", "./dist/server.js"]
    scale: 4
    port_base: 3000
    max_memory_mb: 500
    restart: always
    env:
      NODE_ENV: production
```

## Docker Integration

### Quick Start with Scaffold

The fastest way to get started is using the scaffold command:

```bash
# Next.js - generates config, Dockerfile, nginx.conf
cbox-init scaffold nextjs --dockerfile --nginx --docker-compose

# Nuxt 3
cbox-init scaffold nuxt --dockerfile --nginx --docker-compose

# Generic Node.js (Express, Fastify, NestJS)
cbox-init scaffold nodejs --dockerfile --nginx --docker-compose
```

This generates:
- `cbox-init.yaml` - Process manager configuration
- `Dockerfile` - Multi-stage build with Cbox Init
- `nginx.conf` - Reverse proxy with upstream load balancing
- `docker-compose.yml` - Full stack with PostgreSQL, Redis

### Generated Dockerfile (Next.js example)

```dockerfile
# Multi-stage build optimized for Next.js
FROM node:22-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm ci

FROM node:22-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:22-alpine AS runner
# Install nginx and Cbox Init
RUN apk add --no-cache nginx curl tini
ARG CBOX_INIT_VERSION=latest
ARG TARGETARCH
RUN curl -fsSL "https://github.com/cboxdk/init/releases/${CBOX_INIT_VERSION}/download/cbox-init-linux-${TARGETARCH}" \
    -o /usr/local/bin/cbox-init && chmod +x /usr/local/bin/cbox-init

WORKDIR /app
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public
COPY cbox-init.yaml /etc/cbox-init/config.yaml
COPY nginx.conf /etc/nginx/nginx.conf

ENV NODE_ENV=production HOSTNAME=0.0.0.0
EXPOSE 80 9180 9090

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/local/bin/cbox-init", "serve", "--config", "/etc/cbox-init/config.yaml"]
```

### Generated nginx.conf (Load Balancing)

```nginx
upstream nodejs_backend {
    least_conn;
    keepalive 32;
    server 127.0.0.1:3000 weight=1 max_fails=3 fail_timeout=30s;
    server 127.0.0.1:3001 weight=1 max_fails=3 fail_timeout=30s;
}

server {
    listen 80;

    location /_next/static {
        alias /app/.next/static;
        expires 1y;
        add_header Cache-Control "public, immutable";
    }

    location / {
        proxy_pass http://nodejs_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### docker-compose.yml

```yaml
services:
  app:
    build: .
    ports:
      - "80:80"
      - "9090:9090"  # Metrics
      - "9180:9180"  # API
    environment:
      - NODE_ENV=production
      - DATABASE_URL=postgresql://postgres:secret@db:5432/myapp
      - REDIS_URL=redis://redis:6379
    depends_on:
      - db
      - redis
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:80/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: myapp
    volumes:
      - db-data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis-data:/data

volumes:
  db-data:
  redis-data:
```

## Scaffold Command

Generate configurations quickly:

```bash
# Next.js preset with all files
cbox-init scaffold nextjs --dockerfile --nginx --docker-compose

# Nuxt preset
cbox-init scaffold nuxt --dockerfile --nginx

# Generic Node.js
cbox-init scaffold nodejs --dockerfile --nginx

# Interactive mode (guided prompts)
cbox-init scaffold --interactive

# Custom app name and output directory
cbox-init scaffold nextjs --app-name my-nextjs-app --output ./docker
```

## Best Practices

1. **Always implement graceful shutdown** - Handle SIGTERM in your app
2. **Use health checks** - Ensure proper readiness before accepting traffic
3. **Set memory limits** - Protect against memory leaks with `max_memory_mb`
4. **Use JSON logging** - Structured logs integrate better with monitoring
5. **Scale appropriately** - Use `scale` + `port_base` for horizontal scaling
6. **Separate concerns** - Use different processes for API, workers, and schedulers

## Example Configurations

Full example configurations are available in `configs/examples/`:

- `nextjs.yaml` - Next.js with standalone mode
- `nuxt.yaml` - Nuxt 3 with Nitro server
- `nodejs-generic.yaml` - Generic Node.js application
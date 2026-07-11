---
title: "Kubernetes Deployment"
description: "Deploy PHP applications with Cbox Init on Kubernetes with ConfigMaps, HPA, and resource limits"
weight: 35
---

# Kubernetes Deployment

Deploy PHP applications with Cbox Init on Kubernetes using ConfigMaps for configuration and HorizontalPodAutoscaler for dynamic scaling.

## Use Cases

- ✅ Production Kubernetes deployments
- ✅ Multi-environment configuration (dev/staging/prod)
- ✅ Auto-scaling based on CPU/memory
- ✅ ConfigMap-based PHP-FPM tuning
- ✅ Zero-downtime deployments

## Architecture Overview

```
┌────────────────────────────────────────────┐
│  Kubernetes Namespace: production          │
│                                             │
│  ┌──────────────────────────────────────┐ │
│  │  ConfigMap: cbox-config            │ │
│  │  - php_fpm_profile: medium           │ │
│  │  - cbox_init_config: (YAML)          │ │
│  └──────────────────────────────────────┘ │
│                                             │
│  ┌──────────────────────────────────────┐ │
│  │  Deployment: php-app (replicas:3)│ │
│  │  ┌──────────────┐                    │ │
│  │  │  Pod 1       │  PHP-FPM + Nginx   │ │
│  │  ├──────────────┤  + Horizon + Queue │ │
│  │  │  Pod 2       │  (2Gi RAM, 2 CPU)  │ │
│  │  ├──────────────┤                    │ │
│  │  │  Pod 3       │  Auto-tuned PHP    │ │
│  │  └──────────────┘                    │ │
│  └──────────────────────────────────────┘ │
│                                             │
│  ┌──────────────────────────────────────┐ │
│  │  HPA: php-app-hpa                │ │
│  │  Min: 3, Max: 10                     │ │
│  │  Target: 70% CPU, 80% Memory         │ │
│  └──────────────────────────────────────┘ │
│                                             │
│  ┌──────────────────────────────────────┐ │
│  │  Service: php-app-svc            │ │
│  │  Type: ClusterIP                     │ │
│  │  Port: 80                            │ │
│  └──────────────────────────────────────┘ │
└────────────────────────────────────────────┘
```

## Complete Kubernetes Configuration

### ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cbox-config
  namespace: production
data:
  # PHP-FPM auto-tune profile
  php_fpm_profile: "medium"

  # Cbox Init configuration
  cbox_init_config: |
    version: "1.0"
    global:
      shutdown_timeout: 60
      log_format: json
      log_level: info
      metrics_enabled: true
      metrics_port: 9090

    hooks:
      pre-start:
        - name: config-cache
          command: ["php", "artisan", "config:cache"]
          timeout: 60

        - name: migrate
          command: ["php", "artisan", "migrate", "--force"]
          timeout: 300

    processes:
      php-fpm:
        enabled: true
        command: ["php-fpm", "-F", "-R"]
        restart: always
        health_check:
          type: tcp
          address: 127.0.0.1:9000

      nginx:
        enabled: true
        command: ["nginx", "-g", "daemon off;"]
        depends_on: [php-fpm]
        health_check:
          type: http
          url: http://localhost/health

      horizon:
        enabled: true
        command: ["php", "artisan", "horizon"]
        shutdown:
          pre_stop_hook:
            command: ["php", "artisan", "horizon:terminate"]
            timeout: 60

      queue-default:
        enabled: true
        command: ["php", "artisan", "queue:work"]
        scale: 2
```

### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app
  namespace: production
  labels:
    app: php
    tier: backend
spec:
  replicas: 3
  selector:
    matchLabels:
      app: php
  template:
    metadata:
      labels:
        app: php
        tier: backend
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      containers:
      - name: app
        image: myapp:v1.2.3
        imagePullPolicy: IfNotPresent

        # PHP-FPM auto-tuning from ConfigMap
        env:
          - name: PHP_FPM_AUTOTUNE_PROFILE
            valueFrom:
              configMapKeyRef:
                name: cbox-config
                key: php_fpm_profile

          # Application environment
          - name: APP_ENV
            value: "production"

          - name: APP_KEY
            valueFrom:
              secretKeyRef:
                name: app-secrets
                key: app-key

          - name: DB_HOST
            value: "mysql-service"

          - name: REDIS_HOST
            value: "redis-service"

        # Mount Cbox Init config
        volumeMounts:
          - name: cbox-config
            mountPath: /etc/cbox-init
            readOnly: true

        # Container resource limits (auto-tuner uses these)
        resources:
          requests:
            memory: "1Gi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "2"

        # Ports
        ports:
          - containerPort: 80
            name: http
            protocol: TCP
          - containerPort: 9090
            name: metrics
            protocol: TCP

        # Readiness probe
        readinessProbe:
          httpGet:
            path: /health
            port: 80
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          successThreshold: 1
          failureThreshold: 3

        # Liveness probe
        livenessProbe:
          httpGet:
            path: /health
            port: 80
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3

      # Volumes
      volumes:
        - name: cbox-config
          configMap:
            name: cbox-config
            items:
              - key: cbox_init_config
                path: cbox-init.yaml

      # Security
      securityContext:
        fsGroup: 1000
        runAsUser: 1000
        runAsNonRoot: true
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: php-app-svc
  namespace: production
  labels:
    app: php
spec:
  type: ClusterIP
  ports:
    - port: 80
      targetPort: 80
      protocol: TCP
      name: http
    - port: 9090
      targetPort: 9090
      protocol: TCP
      name: metrics
  selector:
    app: php
```

### HorizontalPodAutoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: php-app-hpa
  namespace: production
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: php-app
  minReplicas: 3
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70  # Scale up at 70% CPU

    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80  # Scale up at 80% memory
```

### Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: php-app-ingress
  namespace: production
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
    - hosts:
        - myapp.com
      secretName: myapp-tls
  rules:
    - host: myapp.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: php-app-svc
                port:
                  number: 80
```

## Multi-Environment Setup

### Development Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app-dev
  namespace: development
spec:
  replicas: 1  # Single replica for dev
  template:
    spec:
      containers:
      - name: app
        image: myapp:dev
        env:
          - name: PHP_FPM_AUTOTUNE_PROFILE
            value: "dev"  # Minimal resources

          - name: CBOX_INIT_GLOBAL_LOG_LEVEL
            value: "debug"

          - name: CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE
            value: "1"

        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
```

### Production with Heavy Traffic

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app-prod
  namespace: production
spec:
  replicas: 5
  template:
    spec:
      containers:
      - name: app
        image: myapp:v1.2.3
        env:
          - name: PHP_FPM_AUTOTUNE_PROFILE
            value: "heavy"  # High traffic profile

          - name: CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE
            value: "10"

        resources:
          limits:
            memory: "8Gi"
            cpu: "8"
```

## Pod Scaling Strategy

### Two-Layer Scaling

**Layer 1: Kubernetes HPA (Pod-level)**
```yaml
# Scales number of pods
HPA:
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilization: 70%
```

**Layer 2: PHP-FPM Auto-Tuning (Per-pod)**
```yaml
# Scales workers within each pod
PHP_FPM_AUTOTUNE_PROFILE: medium
# With 2Gi memory → ~16 workers per pod
```

**Total Capacity:**
- 3 pods × 16 workers = 48 concurrent PHP requests (minimum)
- 10 pods × 16 workers = 160 concurrent PHP requests (maximum)

### Queue Worker Scaling

```yaml
# Per-pod queue workers
CBOX_INIT_PROCESS_QUEUE_DEFAULT_SCALE: 3

# With HPA
# Minimum: 3 pods × 3 workers = 9 total workers
# Maximum: 10 pods × 3 workers = 30 total workers
```

## Monitoring Integration

### Prometheus Scraping

```yaml
# Pod annotations
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "9090"
    prometheus.io/path: "/metrics"
```

**ServiceMonitor:**
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: php-app-metrics
  namespace: production
spec:
  selector:
    matchLabels:
      app: php
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
```

### Grafana Dashboard

```bash
# Import dashboard
# Metrics available:
# - cbox_init_process_up
# - cbox_init_process_restarts_total
# - cbox_init_process_health_status
# - cbox_init_manager_uptime_seconds
```

## Resource Planning

### Memory Calculation

**Per Pod with Medium Profile:**
```
Container memory: 2Gi
PHP-FPM auto-tune: medium profile

Calculation:
- Available: 2048 × 0.75 = 1536MB
- Reserved: 192MB (system) + 128MB (OPcache) = 320MB
- Worker memory: 1536 - 320 = 1216MB
- Workers: 1216 / 42MB = ~28 workers
- CPU limit: 2 cores × 4 = 8 workers (CPU LIMITED)
- Final: 8 workers per pod

With HPA (3-10 pods):
- Minimum capacity: 3 × 8 = 24 workers
- Maximum capacity: 10 × 8 = 80 workers
```

### CPU Allocation

```yaml
resources:
  requests:
    cpu: "500m"  # 0.5 cores guaranteed
  limits:
    cpu: "2"     # Max 2 cores
```

**Recommendations:**
- `requests`: What pod needs to run (guaranteed)
- `limits`: Maximum pod can use (bursts)
- Set `requests` to 50-70% of `limits`

## Deployment Strategies

### Rolling Update (Zero Downtime)

```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  replicas: 5
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1  # Keep at least 4 pods running
      maxSurge: 2        # Add up to 2 extra pods during update
```

**Update flow:**
1. Create 2 new pods (total: 7)
2. Wait for new pods to be healthy
3. Terminate 1 old pod (total: 6)
4. Repeat until all old pods replaced

### Blue-Green Deployment

```yaml
# Blue deployment (current)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app-blue
spec:
  replicas: 5
  selector:
    matchLabels:
      app: php
      version: blue

# Green deployment (new)
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app-green
spec:
  replicas: 5
  selector:
    matchLabels:
      app: php
      version: green

# Service switches between blue/green
---
apiVersion: v1
kind: Service
metadata:
  name: php-app-svc
spec:
  selector:
    app: php
    version: blue  # Change to 'green' to switch
```

## Secrets Management

### Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
  namespace: production
type: Opaque
stringData:
  app-key: base64:your-app-key-here
  db-password: your-db-password
  redis-password: your-redis-password
  api-token: your-cbox-api-token
```

**Use in Deployment:**
```yaml
env:
  - name: APP_KEY
    valueFrom:
      secretKeyRef:
        name: app-secrets
        key: app-key

  - name: CBOX_INIT_GLOBAL_API_AUTH
    valueFrom:
      secretKeyRef:
        name: app-secrets
        key: api-token
```

### External Secrets Operator

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: app-secrets
spec:
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: app-secrets
  data:
    - secretKey: app-key
      remoteRef:
        key: production/laravel/app-key

    - secretKey: db-password
      remoteRef:
        key: production/database/password
```

## Multi-Profile Deployments

### Standard Profile

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app
spec:
  template:
    spec:
      containers:
      - name: app
        env:
          - name: PHP_FPM_AUTOTUNE_PROFILE
            value: "medium"
        resources:
          limits:
            memory: "2Gi"
            cpu: "2"
```

### Heavy Traffic Profile

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app-heavy
spec:
  template:
    spec:
      containers:
      - name: app
        env:
          - name: PHP_FPM_AUTOTUNE_PROFILE
            value: "heavy"
        resources:
          limits:
            memory: "8Gi"
            cpu: "8"
```

## Persistent Storage

### Logs Volume

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: app-logs
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
---
# Mount in Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        volumeMounts:
          - name: logs
            mountPath: /var/www/storage/logs
      volumes:
        - name: logs
          persistentVolumeClaim:
            claimName: app-logs
```

### Shared Storage (Sessions, Cache)

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: app-storage
spec:
  accessModes:
    - ReadWriteMany  # Multiple pods can write
  storageClassName: efs  # AWS EFS or similar
  resources:
    requests:
      storage: 50Gi
```

## Health Checks

### Kubernetes Probes

```yaml
# Readiness: Is pod ready to receive traffic?
readinessProbe:
  httpGet:
    path: /health
    port: 80
  initialDelaySeconds: 10
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3

# Liveness: Is pod still alive?
livenessProbe:
  httpGet:
    path: /health
    port: 80
  initialDelaySeconds: 30
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3
```

**How they work together:**
- **Readiness:** Removes pod from Service when unhealthy
- **Liveness:** Restarts pod if continuously unhealthy
- Both use Cbox Init's health endpoint

## Complete Deployment

```bash
# Apply all resources
kubectl apply -f - <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: production
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cbox-config
  namespace: production
data:
  php_fpm_profile: "medium"
  cbox_init_config: |
    version: "1.0"
    # ... (full config from above)
---
apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
  namespace: production
type: Opaque
stringData:
  app-key: ${APP_KEY}
  db-password: ${DB_PASSWORD}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: php-app
  namespace: production
spec:
  # ... (full deployment from above)
---
apiVersion: v1
kind: Service
metadata:
  name: php-app-svc
  namespace: production
spec:
  # ... (full service from above)
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: php-app-hpa
  namespace: production
spec:
  # ... (full HPA from above)
EOF
```

## Deployment Commands

```bash
# Create namespace
kubectl create namespace production

# Apply configuration
kubectl apply -f configmap.yaml
kubectl apply -f secrets.yaml
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f hpa.yaml
kubectl apply -f ingress.yaml

# Verify deployment
kubectl get pods -n production
kubectl get svc -n production
kubectl get hpa -n production

# Check logs
kubectl logs -n production deployment/php-app -f

# Check pod health
kubectl get pods -n production -o wide

# Describe pod
kubectl describe pod -n production php-app-xxx
```

## Monitoring

### Check Metrics

```bash
# Port-forward to metrics endpoint
kubectl port-forward -n production deployment/php-app 9090:9090

# Query metrics
curl http://localhost:9090/metrics
```

### Check API

```bash
# Port-forward to API
kubectl port-forward -n production deployment/php-app 8080:8080

# Get process status
curl http://localhost:9180/api/v1/processes
```

### View Logs

```bash
# All pods
kubectl logs -n production -l app=laravel --tail=100 -f

# Specific pod
kubectl logs -n production php-app-xxx -f

# Previous container (if crashed)
kubectl logs -n production php-app-xxx --previous
```

## Troubleshooting

### Pods Not Scaling

**Check HPA status:**
```bash
kubectl get hpa -n production
kubectl describe hpa -n production php-app-hpa
```

**Common issues:**
- Metrics server not installed
- Resource requests not set
- Target metrics unreachable

### OOM Kills

**Symptom:**
```bash
kubectl describe pod php-app-xxx
# Reason: OOMKilled
```

**Solutions:**
```yaml
# Option 1: Increase memory limits
resources:
  limits:
    memory: "4Gi"  # Was 2Gi

# Option 2: Reduce PHP-FPM workers
env:
  - name: PHP_FPM_AUTOTUNE_PROFILE
    value: "light"  # Was medium
```

### Readiness Probe Failing

**Check pod logs:**
```bash
kubectl logs -n production php-app-xxx | grep health
```

**Adjust probe:**
```yaml
readinessProbe:
  initialDelaySeconds: 30  # Was 10, app needs more time
  failureThreshold: 5  # Was 3, tolerate more failures
```

### ConfigMap Not Updating

**Problem:** Changed ConfigMap but pods still use old config

**Solution:** Restart pods to pick up changes
```bash
kubectl rollout restart deployment/php-app -n production
```

## Best Practices

### Resource Limits

```yaml
# Always set requests and limits
resources:
  requests:    # Guaranteed resources
    memory: "1Gi"
    cpu: "500m"
  limits:      # Maximum allowed
    memory: "2Gi"
    cpu: "2"
```

### Health Probes

```yaml
# Start liveness later than readiness
readinessProbe:
  initialDelaySeconds: 10

livenessProbe:
  initialDelaySeconds: 30  # Give app time to start
```

### Pod Disruption Budget

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: php-app-pdb
spec:
  minAvailable: 2  # Always keep at least 2 pods running
  selector:
    matchLabels:
      app: php
```

### Security Context

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  fsGroup: 1000
  capabilities:
    drop:
      - ALL
```

## See Also

- [PHP-FPM Auto-Tuning](../configuration/php-fpm-autotune) - Worker optimization for K8s
- [Docker Integration](../getting-started/docker-integration) - Container patterns
- [Health Checks](../configuration/health-checks) - Probe configuration
- [Prometheus Metrics](../observability/metrics) - Metrics integration

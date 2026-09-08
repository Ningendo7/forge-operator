# Observability

Three independent, all-opt-in layers: Prometheus metrics + alerts, a Grafana dashboard, and OpenTelemetry tracing. None of them are required for the operator to function — they exist to answer "is storage provisioning actually healthy" and "why is this reconcile slow/failing" without reading operator logs.

## Metrics

Domain-specific metrics (distinct from controller-runtime's own built-in reconcile/workqueue metrics, which are already exposed) are defined in [internal/controller/observability/metrics.go](../internal/controller/observability/metrics.go) and served on the same `/metrics` endpoint as everything else (see `metrics.enabled`/`metrics.secure` in [Installation & Configuration](installation-and-configuration.md#helm-values)):

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `forge_storage_reconcile_total` | Counter | `provider`, `outcome` | Every storage reconcile attempt. `outcome` is one of `ready`, `not_owned`, `timeout`, `access_denied`, `other_error` — a small, fixed set (see [internal/controller/errorclass.go](../internal/controller/errorclass.go)), never raw error text, so it stays low-cardinality and safe to alert on |
| `forge_storage_reconcile_duration_seconds` | Histogram | `provider` | Time spent in the cloud-call-bound part of a storage reconcile, bucketed out to 90s (`storageReconcileTimeout`) |
| `forge_storage_ready` | Gauge | `namespace`, `name`, `provider` | 1/0 per `Application` with `spec.storage` set — its current `StorageReady` condition |
| `forge_storage_bucket_adopted_total` | Counter | `provider` | Incremented every time the adopt-bucket annotation actually results in a claim — an audit signal, not an incident (see [Ownership verification](authentication-and-storage.md#ownership-verification)) |
| `forge_finalizer_cleanup_total` | Counter | `provider`, `outcome` | Every storage cleanup attempt during `Application` deletion, same outcome set as above minus `not_owned` (cleanup doesn't re-verify ownership) |
| `forge_finalizer_cleanup_duration_seconds` | Histogram | `provider` | Time spent in cleanup during finalization |
| `forge_application_ready` | Gauge | `namespace`, `name` | 1/0 per `Application` — its overall `Ready` condition, independent of storage |

## Prometheus: ServiceMonitor and alerts

Two independent Helm flags:

- `prometheus.enabled` (default `false`) — installs a `ServiceMonitor` so a prometheus-operator-based Prometheus scrapes `/metrics` at all.
- `prometheus.rules.enabled` (default `false`) — installs a `PrometheusRule` with a curated set of alerts (see [charts/chart/templates/prometheus/controller-manager-alert-rules.yaml](../charts/chart/templates/prometheus/controller-manager-alert-rules.yaml) for the authoritative, up-to-date list — currently covers sustained storage reconcile failures, reconcile latency approaching timeout, a stuck finalizer, access-denied errors, bucket adoption (audit), Kubernetes API error rate, `Application`s stuck `Degraded`, and reconcile concurrency/workqueue backlog). Deliberately separate from `prometheus.enabled`: scraping metrics is harmless, turning on alerts that can page someone is a decision worth making explicitly. Requires `prometheus.enabled: true` for the underlying metrics to actually be scraped.

If you're running [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack), its Prometheus/Alertmanager CRs default `serviceMonitorSelector`/`ruleSelector` to `matchLabels: {release: <kube-prometheus-stack's own release name>}` — without a matching label, `prometheus.enabled: true` silently scrapes nothing. Set `prometheus.additionalLabels` to close that gap:

```yaml
prometheus:
  enabled: true
  additionalLabels:
    release: <kube-prometheus-stack-release-name>
  rules:
    enabled: true
```

The `metrics-reader` ClusterRole this chart creates (for `metrics.secure: true`) is bound to nobody by default — list the ServiceAccount(s) that need to scrape `/metrics` (e.g. kube-prometheus-stack's own Prometheus ServiceAccount) via `metrics.reader.serviceAccountBindings`:

```yaml
metrics:
  reader:
    serviceAccountBindings:
      - name: kube-prometheus-stack-prometheus
        namespace: monitoring
```

## Grafana dashboard

`grafana.dashboard.enabled` (default `false`) ships a dashboard covering the same ground as the alerts above, as a ConfigMap labeled `grafana_dashboard: "1"` (see [charts/chart/templates/grafana/controller-manager-dashboard.yaml](../charts/chart/templates/grafana/controller-manager-dashboard.yaml)). kube-prometheus-stack's Grafana runs a sidecar that auto-discovers dashboards carrying that label across all namespaces by default, so this needs no extra wiring on a standard install — if your Grafana's sidecar is scoped to specific namespaces instead, add this chart's release namespace to it. Requires `prometheus.enabled: true` for the underlying metrics to exist.

## Tracing

OpenTelemetry tracing (via [internal/controller/observability/tracing.go](../internal/controller/observability/tracing.go)) is opt-in and env-var-gated, wired in [cmd/main.go](../cmd/main.go): set `OTEL_EXPORTER_OTLP_ENDPOINT` (standard OTel env var, read by the SDK itself — not a Forge-specific setting) via `manager.env`, and the operator exports spans over OTLP/gRPC. Unset, tracing is not merely disabled but a true no-op at the OpenTelemetry API level — every `Tracer().Start()` call becomes free, with zero network attempts.

```yaml
manager:
  env:
    - name: OTEL_EXPORTER_OTLP_ENDPOINT
      value: "http://jaeger.jaeger.svc.cluster.local:4317"
```

For trying this out, [Jaeger's own all-in-one image](https://www.jaegertracing.io/docs/latest/getting-started/#all-in-one) needs only a Deployment exposing container port `4317` (OTLP/gRPC, with `COLLECTOR_OTLP_ENABLED=true` set) and port `16686` (its UI) behind a Service — fine for local/dev tracing, not meant as a production tracing backend (no persistence, no HA).

## Reduced Secret cache footprint

Unrelated to metrics/tracing but a related piece of operational hardening from the same work: the controller's informer cache for `Secret` objects is scoped to only the Secrets this operator itself creates and labels (see `SecretRoleLabel` in [cmd/main.go](../cmd/main.go)), not every Secret cluster-wide. Reads of Secrets the operator doesn't own (`spec.storage.secretName`, `spec.storage.akamai.accessKeySecretRef`) bypass the cache entirely and go straight to the API server. This narrows how many arbitrary, unrelated Secrets this operator's pod holds a live copy of in memory — RBAC still permits cluster-wide Secret access (a separate, larger decision), but the cache no longer does.

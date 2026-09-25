# Installation

## Prerequisites

- Kubernetes cluster
- kubectl
- Go 1.26+
- Docker
- Terraform (only if you're also standing up the underlying cloud infrastructure — see [Terraform Infrastructure](#terraform-infrastructure))
- Make
- Helm (recommended install path) or Kustomize (alternative, below)

## Deploy via Helm (recommended)

```sh
helm install forge-operator oci://ghcr.io/ningendo7/forge-operator/charts/forge-operator \
  --version <released-version> \
  --namespace forge-operator-system \
  --create-namespace \
  --set manager.image.tag=<released-version>
```

If you're deploying against AWS, see [Wiring the controller's own IRSA role after `terraform apply`](authentication-and-storage.md#wiring-the-controllers-own-irsa-role-after-terraform-apply) — the controller's pod needs its own IRSA role ARN passed in via `--set`, which isn't something the chart can default for you.

See [Configuration](#configuration) for the other values worth overriding.

## Deploy via Kustomize (alternative)

```sh
make manifests
make install
make deploy IMG=<registry>/forge-operator:<tag>
```

## Verify

```sh
kubectl get pods -n forge-operator-system
kubectl apply -f config/samples/forge_v1alpha1_application.yaml
kubectl get applications
```

# Configuration

## Helm values

Full reference: [charts/chart/values.yaml](../charts/chart/values.yaml). The ones you're most likely to touch:

| Value | Purpose |
| --- | --- |
| `manager.image.repository` / `manager.image.tag` | Controller image; the release workflow points this at the tagged GHCR image automatically |
| `manager.replicas` | Controller pod count (leader election, not `Application` replicas — see below) |
| `manager.args` | Extra manager flags, e.g. `--leader-elect` (already set by default) |
| `serviceAccount.annotations` | e.g. `eks\.amazonaws\.com/role-arn` for the controller's own AWS IRSA role — see [Authentication Flows](authentication-and-storage.md#wiring-the-controllers-own-irsa-role-after-terraform-apply) |
| `rbac.namespaced` | `false` (default) = ClusterRole covering all namespaces; `true` = Role scoped to the release namespace only — note this can make the [adopt-bucket](authentication-and-storage.md#explicit-adoption) previous-owner check fail closed across namespaces |
| `crd.keep` | Keep CRDs on `helm uninstall` (default `true`, so deleting the release never silently deletes your `Application` resources) |
| `metrics.enabled` / `metrics.secure` | Expose the `/metrics` endpoint, optionally behind authn/authz |
| `webhook.enabled` / `webhook.port` | Register the Application admission webhooks (default `true`) — see [Webhooks](architecture.md#webhooks) |
| `certManager.enabled` | Use cert-manager for the webhook server's and metrics endpoint's TLS certificates (default `true`); required for `webhook.enabled` to actually work, since `failurePolicy: Fail` means an untrusted cert blocks every `Application` create/update |
| `prometheus.enabled` / `prometheus.rules.enabled` / `prometheus.additionalLabels` | Install a `ServiceMonitor` and/or a curated `PrometheusRule`, and merge extra labels onto both (needed for kube-prometheus-stack's default selectors) — see [Observability](observability.md#prometheus-servicemonitor-and-alerts) |
| `grafana.dashboard.enabled` | Install a Grafana dashboard ConfigMap (auto-discovered by kube-prometheus-stack's Grafana sidecar) — see [Observability](observability.md#grafana-dashboard) |
| `metrics.reader.serviceAccountBindings` | ServiceAccounts granted the `metrics-reader` ClusterRole, e.g. Prometheus's own ServiceAccount when `metrics.secure: true` — see [Observability](observability.md#prometheus-servicemonitor-and-alerts) |
| `manager.env` | Extra environment variables on the manager container — this is how you set the variables below |

## Controller environment variables

Set via `manager.env` in Helm values:

```yaml
manager:
  env:
    - name: OIDC_PROVIDER_ARN
      value: "arn:aws:iam::123456789012:role/example"
    - name: OIDC_PROVIDER_URL
      value: "oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"
    - name: DEFAULT_AKAMAI_REGION
      value: "us-iad"
```

| Variable | Purpose |
| --- | --- |
| `OIDC_PROVIDER_ARN` | Required for AWS IRSA role trust policies |
| `OIDC_PROVIDER_URL` | Required for AWS IRSA role trust policies |
| `DEFAULT_AKAMAI_REGION` | Fallback region for Akamai/Linode storage when an `Application` doesn't set `spec.storage.region` itself (which always takes precedence). Should match wherever your Akamai/Linode infrastructure actually runs — there's no built-in default; an unset default plus an unset `spec.storage.region` fails loudly with a clear error from Linode's API rather than silently guessing a region |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Standard OpenTelemetry env var — set it to enable OTLP/gRPC trace export (e.g. to Jaeger). Unset (the default) means tracing is a true no-op, not just "disabled" — zero network calls. See [Observability](observability.md#tracing) |
| `MAX_CONCURRENT_RECONCILES` | How many `Application`s this controller reconciles in parallel. Defaults to `5`; an unset or `<= 0` value falls back to that default rather than failing startup. Independent of the rate limiters below — this bounds reconcile parallelism, not external call rate. Watch `controller_runtime_active_workers`/`controller_runtime_max_concurrent_reconciles` (the `ForgeReconcileConcurrencySaturated` alert, see [Observability](observability.md#prometheus-servicemonitor-and-alerts)) to see whether this needs raising |

If deploying via kustomize instead of Helm, there's no built-in mechanism for this — add your own patch targeting `spec.template.spec.containers[0].env` (see [`config/default/manager_webhook_patch.yaml`](../config/default/manager_webhook_patch.yaml) for the JSON-patch style already used there).

### Rate limiting

The operator paces its own outgoing AWS/Akamai calls independent of `MaxConcurrentReconciles` — that bounds how many reconciles run in parallel, not how many external API calls they collectively make per second. At fleet scale (a mass resync after a restart, a cluster-wide spec change touching every `Application` at once), several concurrent reconciles each making a handful of sequential AWS/Akamai calls can genuinely burst past a provider's real rate limits, especially AWS IAM's, which are far tighter than S3's.

S3, IAM, Akamai's account API, and Akamai's S3-compatible object endpoint each get their own independent token-bucket limiter (see [internal/controller/ratelimit](../internal/controller/ratelimit/ratelimit.go)) rather than one shared budget — sharing one across any of them would mean the tightest one throttles the others down to its own ceiling for no reason tied to their actual capacity. All four have built-in defaults and don't require any configuration to use; the env vars below only need setting to tune them for a specific AWS/Linode account's real limits:

```yaml
manager:
  env:
    - name: AWS_S3_RATE_LIMIT_QPS
      value: "20"
    - name: AWS_S3_RATE_LIMIT_BURST
      value: "40"
    - name: AWS_IAM_RATE_LIMIT_QPS
      value: "8"
    - name: AWS_IAM_RATE_LIMIT_BURST
      value: "16"
    - name: AKAMAI_ACCOUNT_RATE_LIMIT_QPS
      value: "5"
    - name: AKAMAI_ACCOUNT_RATE_LIMIT_BURST
      value: "10"
    - name: AKAMAI_OBJECT_RATE_LIMIT_QPS
      value: "20"
    - name: AKAMAI_OBJECT_RATE_LIMIT_BURST
      value: "40"
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `AWS_S3_RATE_LIMIT_QPS` / `AWS_S3_RATE_LIMIT_BURST` | 20 / 40 | S3 bucket calls (create, head, versioning, lifecycle, tagging, deletion) |
| `AWS_IAM_RATE_LIMIT_QPS` / `AWS_IAM_RATE_LIMIT_BURST` | 8 / 16 | IAM role/policy calls for IRSA — deliberately tighter than S3, matching AWS IAM's own lower real-world limits |
| `AKAMAI_ACCOUNT_RATE_LIMIT_QPS` / `AKAMAI_ACCOUNT_RATE_LIMIT_BURST` | 5 / 10 | Akamai/Linode's account-level v4 REST API (bucket/key CRUD) |
| `AKAMAI_OBJECT_RATE_LIMIT_QPS` / `AKAMAI_OBJECT_RATE_LIMIT_BURST` | 20 / 40 | Akamai's S3-compatible object endpoint (ownership marker reads/writes) |

Values are deliberately conservative starting points, not verified figures for any specific account tier. An unset or unparseable value falls back to its default rather than failing startup; a value `<= 0` falls back further still to an absolute floor of 1 QPS / burst 1, never to "unlimited" or "blocks forever." Watch `forge_rate_limit_wait_duration_seconds` (see [Observability](observability.md#metrics)) to see whether a given surface's defaults have headroom before tuning them.

### Storage resync jitter

| Variable | Default | Purpose |
| --- | --- | --- |
| `STORAGE_RESYNC_INTERVAL` | `10m` | How often a settled, storage-backed `Application` re-verifies its cloud bucket still exists. Parsed via Go's [`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) (e.g. `15s`, `10m`); unset or unparseable falls back to the default. |

Each resync adds up to +20% random jitter on top of this interval, so many `Application`s created around the same time (a bulk apply, or everything created right after cluster bootstrap) don't all resync in the same narrow window every cycle — this spreads the periodic `HeadBucket`/`GetObject` load out instead of clustering it into periodic spikes, the same reasoning as the rate limiters above but for request *timing* rather than request *rate*. Jitter is additive only, so a resync is never shorter than the configured interval, only occasionally longer.

## Leader election

Enabled by default in the Helm chart's `manager.args` (`--leader-elect`), so multiple replicas run active/standby safely. Runtime wiring is in [cmd/main.go](../cmd/main.go).

### Rate limit ramp-up on election

A freshly-elected leader can face a large backlog of `Application`s to reconcile all at once (informer cache sync, then the workqueue draining) — exactly when the rate limiters above are most likely to be hit, right after they were just recreated at full burst. On `mgr.Elected()`, each of the four limiters starts at 10% of its configured target and ramps back up to that target over 30 seconds (see [internal/controller/ratelimit/rampup.go](../internal/controller/ratelimit/rampup.go)), rather than admitting the whole backlog at full burst the instant leadership is acquired. This is local to each limiter instance — a new leader always starts its own ramp from scratch, since the token bucket itself isn't shared state carried over from whichever replica held the lease before it.

# Terraform Infrastructure

Both cloud trees follow the same `modules/` + `environments/{dev,prod}/` layout, with variable *schema* (no defaults) in each environment's `variables.tf` and all actual values explicit in `dev.tfvars`/`prod.tfvars` — so the two environments' variable declarations stay byte-identical and the only thing that differs is what's in the `.tfvars` file:

```sh
terraform apply -var-file=dev.tfvars   # or prod.tfvars
```

- **AWS** — [Terraform/AWS/modules](../Terraform/AWS/modules) (VPC, networking, IAM, EKS, IRSA) with complete `dev` and `prod` environments.
- **Akamai/Linode** — [Terraform/Akamai-Linode/modules](../Terraform/Akamai-Linode/modules) (LKE, networking, firewall) with complete `dev` and `prod` environments. `prod`'s LKE node pool has the firewall attached (`firewall_id`); `dev`'s does not.

CI runs `terraform fmt -check` and `terraform validate` (no credentials needed — see [.github/workflows/terraform.yml](../.github/workflows/terraform.yml)) on any PR touching `Terraform/**`. `plan`/`apply` are intentionally left as manual, local operations against your own backend.

**Before tearing down a cluster** that has `Application` resources with storage on it, delete those `Application`s first and confirm their finalizer has completed — see [Deletion](authentication-and-storage.md#deletion) for why: destroying the cluster out from under a still-running `Application` orphans its cloud storage bucket, since the finalizer never gets a chance to run.

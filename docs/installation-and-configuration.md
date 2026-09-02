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
| `rbac.namespaced` | `false` (default) = ClusterRole covering all namespaces; `true` = Role scoped to the release namespace only |
| `crd.keep` | Keep CRDs on `helm uninstall` (default `true`, so deleting the release never silently deletes your `Application` resources) |
| `metrics.enabled` / `metrics.secure` | Expose the `/metrics` endpoint, optionally behind authn/authz |
| `webhook.enabled` / `webhook.port` | Register the Application admission webhooks (default `true`) — see [Webhooks](architecture.md#webhooks) |
| `certManager.enabled` | Use cert-manager for the webhook server's and metrics endpoint's TLS certificates (default `true`); required for `webhook.enabled` to actually work, since `failurePolicy: Fail` means an untrusted cert blocks every `Application` create/update |
| `prometheus.enabled` | Install a `ServiceMonitor` (requires prometheus-operator CRDs) |
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

If deploying via kustomize instead of Helm, there's no built-in mechanism for this — add your own patch targeting `spec.template.spec.containers[0].env` (see [`config/default/manager_webhook_patch.yaml`](../config/default/manager_webhook_patch.yaml) for the JSON-patch style already used there).

## Leader election

Enabled by default in the Helm chart's `manager.args` (`--leader-elect`), so multiple replicas run active/standby safely. Runtime wiring is in [cmd/main.go](../cmd/main.go).

# Terraform Infrastructure

Both cloud trees follow the same `modules/` + `environments/{dev,prod}/` layout, with variable *schema* (no defaults) in each environment's `variables.tf` and all actual values explicit in `dev.tfvars`/`prod.tfvars` — so the two environments' variable declarations stay byte-identical and the only thing that differs is what's in the `.tfvars` file:

```sh
terraform apply -var-file=dev.tfvars   # or prod.tfvars
```

- **AWS** — [Terraform/AWS/modules](../Terraform/AWS/modules) (VPC, networking, IAM, EKS, IRSA) with complete `dev` and `prod` environments.
- **Akamai/Linode** — [Terraform/Akamai-Linode/modules](../Terraform/Akamai-Linode/modules) (LKE, networking, firewall) with complete `dev` and `prod` environments. `prod`'s LKE node pool has the firewall attached (`firewall_id`); `dev`'s does not.

CI runs `terraform fmt -check` and `terraform validate` (no credentials needed — see [.github/workflows/terraform.yml](../.github/workflows/terraform.yml)) on any PR touching `Terraform/**`. `plan`/`apply` are intentionally left as manual, local operations against your own backend.

**Before tearing down a cluster** that has `Application` resources with storage on it, delete those `Application`s first and confirm their finalizer has completed — see [Deletion](authentication-and-storage.md#deletion) for why: destroying the cluster out from under a still-running `Application` orphans its cloud storage bucket, since the finalizer never gets a chance to run.

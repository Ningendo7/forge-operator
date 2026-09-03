# Forge Operator

[![Tests](https://github.com/Ningendo7/forge-operator/actions/workflows/test.yml/badge.svg)](https://github.com/Ningendo7/forge-operator/actions/workflows/test.yml)
[![E2E Tests](https://github.com/Ningendo7/forge-operator/actions/workflows/test-e2e.yml/badge.svg)](https://github.com/Ningendo7/forge-operator/actions/workflows/test-e2e.yml)
[![Lint](https://github.com/Ningendo7/forge-operator/actions/workflows/lint.yml/badge.svg)](https://github.com/Ningendo7/forge-operator/actions/workflows/lint.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Forge Operator is a Kubernetes operator for managing application infrastructure declaratively across cloud providers.

It manages the lifecycle of application workloads, core Kubernetes resources, and cloud-backed object storage through a single `Application` custom resource.

## Supported Providers

| Provider | Compute Target | Object Storage | Authentication Model |
| --- | --- | --- | --- |
| AWS | EKS | Amazon S3 | IAM Roles for Service Accounts (IRSA) |
| Akamai/Linode | LKE (Terraform modules) | Akamai Object Storage | S3-compatible access credentials |

`spec.storage` is optional — omit it entirely for an `Application` with no object storage need at all.

## Quickstart

The chart enables the Application admission webhooks and cert-manager-issued TLS by default, so **[cert-manager](https://cert-manager.io/docs/installation/) must already be installed in the cluster** before you install this chart — otherwise the install will fail (it creates `Certificate`/`Issuer` custom resources that don't exist without cert-manager's CRDs). If you don't want that, pass `--set certManager.enabled=false --set webhook.enabled=false`; see [Webhooks](docs/architecture.md#webhooks) for what you lose by doing that.

After `terraform apply` finishes standing up the cluster (see [Terraform Infrastructure](docs/installation-and-configuration.md#terraform-infrastructure)) and your kubeconfig points at it, [`scripts/bootstrap-cluster.sh`](scripts/bootstrap-cluster.sh) installs cert-manager and metrics-server for you (idempotent — safe to re-run, skips whatever's already installed):

```sh
./scripts/bootstrap-cluster.sh
```

Neither EKS nor LKE ships either by default. cert-manager is required for the chart to install at all (see above). metrics-server isn't required to install the chart, but without it, any `Application`'s `spec.autoscaling` creates a working `HorizontalPodAutoscaler` that can never actually scale — the real Kubernetes HPA controller has no CPU/memory metrics to act on, silently, with no error anywhere. See the script's own header comment for a few other add-ons (Ingress controller, Prometheus Operator CRDs, a NetworkPolicy-enforcing CNI) that are situational enough — which one, or whether at all — that it deliberately doesn't pick one for you.

Install from the published Helm chart (built and pushed by [`release.yml`](.github/workflows/release.yml) on every tagged release):

```sh
helm install forge-operator oci://ghcr.io/ningendo7/forge-operator/charts/forge-operator \
  --version <released-version> \
  --namespace forge-operator-system \
  --create-namespace
```

Apply a minimal `Application`:

```sh
kubectl apply -f config/samples/forge_v1alpha1_application.yaml
```

Check status:

```sh
kubectl get applications
```

See [Installation and Configuration](docs/installation-and-configuration.md) for the full install/upgrade path (including AWS's extra IRSA-wiring step) and every Helm value that matters.

## Documentation

- **[Architecture](docs/architecture.md)** — reconciliation flow, the Application API, replica/autoscaling handoff, admission webhooks, repository layout
- **[Authentication & Storage](docs/authentication-and-storage.md)** — AWS IRSA and Akamai credential flows, bucket ownership verification, drift detection, deletion/cleanup behavior
- **[Installation & Configuration](docs/installation-and-configuration.md)** — Helm/Kustomize install, every Helm value and controller env var, Terraform infrastructure
- **[Development & Operations](docs/development-and-operations.md)** — local dev commands, e2e test coverage, security notes, current limitations

## License

Apache License 2.0 — see [LICENSE](LICENSE).

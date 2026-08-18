#!/usr/bin/env bash
# Post-`terraform apply` cluster bootstrap: installs the cluster-wide
# add-ons forge-operator itself depends on to actually function, as opposed
# to merely install. Neither EKS nor LKE ships these by default.
#
#   - cert-manager: required for the Helm chart to install at all — its
#     default values enable the Application admission webhooks and their
#     cert-manager-issued TLS (see the README's Quickstart section).
#   - metrics-server: not required for `helm install` to succeed, but
#     without it, any Application's `spec.autoscaling` creates a working
#     HorizontalPodAutoscaler object that can never actually scale — the
#     real K8s HPA controller has no CPU/memory metrics to act on. Silent,
#     not an error anywhere, easy to miss.
#
# Other optional add-ons this script deliberately does NOT install, because
# which one (if any) you want is situational rather than a fixed choice:
#   - An Ingress controller, only needed if you use spec.ingress (which one
#     depends on your cloud/preference: nginx-ingress, an ALB controller on
#     EKS, a NodeBalancer-backed one on LKE, etc.)
#   - Prometheus Operator CRDs, only needed if you set the chart's
#     prometheus.enabled=true (default false)
#   - A NetworkPolicy-enforcing CNI, only needed if you set the chart's
#     networkPolicy.enabled=true (default false) — check what your cluster's
#     default CNI actually enforces before turning this on
#
# Deliberately NOT part of Terraform: the helm/kubernetes providers would
# need the cluster's endpoint and auth token, which don't exist yet at the
# same apply that creates the cluster (providers are configured at plan
# time, before resources exist) — cleanly working around that means two
# separate Terraform states/applies. A small standalone script run once your
# kubeconfig points at the new cluster is simpler and avoids that entirely.
#
# Usage:
#   ./scripts/bootstrap-cluster.sh
#
# Prerequisites: kubectl and helm installed, and your current kubectl
# context already pointed at the target cluster (e.g. after
# `terraform output -raw kubeconfig > ~/.kube/config`, or
# `linode-cli lke kubeconfig-view <cluster-id>` for Akamai/Linode).

set -euo pipefail

CERT_MANAGER_NAMESPACE="${CERT_MANAGER_NAMESPACE:-cert-manager}"
# Matches the version already validated elsewhere in this repo
# (test/utils/utils.go's e2e cert-manager install), so both paths are known
# to work against the same release.
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.20.2}"

METRICS_SERVER_NAMESPACE="${METRICS_SERVER_NAMESPACE:-kube-system}"

# install_via_helm <name> <check-crd-or-empty> <repo-name> <repo-url> <chart> <namespace> [extra helm args...]
# Skips cleanly if already installed (checked via a CRD when one applies, a
# Helm release name otherwise), so this script is safe to re-run.
install_via_helm() {
  local name="$1" check_crd="$2" repo_name="$3" repo_url="$4" chart="$5" namespace="$6"
  shift 6

  echo "==> Checking for an existing ${name} install"
  if [ -n "$check_crd" ] && kubectl get crds "$check_crd" >/dev/null 2>&1; then
    echo "${name} CRDs already present — skipping install."
    return 0
  fi
  if helm status "$name" --namespace "$namespace" >/dev/null 2>&1; then
    echo "${name} Helm release already present in namespace '${namespace}' — skipping install."
    return 0
  fi

  echo "==> Installing ${name} into namespace '${namespace}'"
  helm repo add "$repo_name" "$repo_url" >/dev/null
  helm repo update "$repo_name" >/dev/null
  helm install "$name" "${repo_name}/${chart}" \
    --namespace "$namespace" \
    --create-namespace \
    --wait \
    --timeout 5m \
    "$@"
}

echo "==> Checking kubectl connectivity"
if ! kubectl cluster-info >/dev/null 2>&1; then
  echo "error: kubectl cannot reach a cluster. Point your kubeconfig at the target cluster first." >&2
  echo "  (e.g. 'terraform output -raw kubeconfig > ~/.kube/config', or 'linode-cli lke kubeconfig-view <cluster-id>')" >&2
  exit 1
fi
kubectl cluster-info | head -n 1

install_via_helm cert-manager certificates.cert-manager.io jetstack https://charts.jetstack.io cert-manager \
  "$CERT_MANAGER_NAMESPACE" --version "$CERT_MANAGER_VERSION" --set crds.enabled=true

install_via_helm metrics-server "" metrics-server https://kubernetes-sigs.github.io/metrics-server/ metrics-server \
  "$METRICS_SERVER_NAMESPACE"

echo "==> Done. You can now install the forge-operator chart:"
echo "    helm install forge-operator oci://ghcr.io/ningendo7/forge-operator/charts/forge-operator --version <released-version> --namespace forge-operator-system --create-namespace"

#!/usr/bin/env bash
# check-generated-drift.sh catches two classes of drift this project has no
# other automated defense against (see finding #10 in the punch-list memory:
# both were found live, by hand, only after already shipping):
#
# 1. A *_types.go marker or webhook marker changed, but `make manifests
#    generate build-installer` was never re-run/committed -- config/crd/bases,
#    config/rbac/role.yaml, config/webhook/manifests.yaml,
#    zz_generated.deepcopy.go, or dist/install.yaml silently fall behind.
# 2. The Helm chart's hand-maintained copies of the CRD and the manager
#    ClusterRole (charts/chart/templates/crd/*.yaml,
#    charts/chart/templates/rbac/manager-role.yaml -- neither has a
#    generator, per AGENTS.md/the punch-list) silently drift from the
#    kustomize-generated sources they're supposed to mirror. This exact drift
#    shipped for real once already: the Helm chart's ClusterRole was missing
#    the `namespaces: get` rule config/rbac/role.yaml has had for a while,
#    which would 403 the cross-namespace bucket-adoption check on any
#    Helm-deployed operator.
#
# Run with no arguments. Exits non-zero (with a diff) on any drift. Intended
# for CI (see .github/workflows/lint.yml) -- safe to run against a dirty tree
# locally too, but it WILL modify generated files in place via `make
# manifests generate build-installer`, same as running that target yourself.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> Regenerating manifests/deepcopy/installer"
make manifests generate build-installer >/dev/null

echo "==> Checking for uncommitted drift in generated files"
generated_paths=(
	api/v1alpha1/zz_generated.deepcopy.go
	config/crd/bases
	config/rbac/role.yaml
	config/webhook/manifests.yaml
	dist/install.yaml
)
if ! git diff --exit-code -- "${generated_paths[@]}"; then
	echo
	echo "ERROR: generated files are out of date -- run 'make manifests generate build-installer' and commit the result" >&2
	exit 1
fi

# strip_helm_wrapper removes Helm template control lines ({{- ... }}) and any
# lines added purely for chart-specific behavior (the resource-policy
# annotation guard), leaving only content that should be byte-identical to
# the kustomize-generated source it was built from.
strip_helm_wrapper() {
	grep -vE '^\s*\{\{-.*-?\}\}\s*$' "$1" | grep -v 'helm.sh/resource-policy'
}

echo "==> Checking the Helm chart's CRD copy against config/crd/bases"
crd_base="config/crd/bases/forge.ningendo7.github.io_applications.yaml"
crd_chart="charts/chart/templates/crd/applications.forge.ningendo7.github.io.yaml"
if ! diff -u <(grep -v '^---$' "$crd_base") <(strip_helm_wrapper "$crd_chart"); then
	echo
	echo "ERROR: $crd_chart has drifted from $crd_base -- regenerate it from the base (strip the leading '---', wrap with" >&2
	echo "the existing {{- if .Values.crd.enabled }}/{{- if .Values.crd.keep }} guards, append {{- end }}) and commit the result" >&2
	exit 1
fi

echo "==> Checking the Helm chart's RBAC copy against config/rbac/role.yaml"
rbac_base="config/rbac/role.yaml"
rbac_chart="charts/chart/templates/rbac/manager-role.yaml"
if ! diff -u <(sed -n '/^rules:/,$p' "$rbac_base") <(sed -n '/^rules:/,$p' "$rbac_chart"); then
	echo
	echo "ERROR: $rbac_chart's rules have drifted from $rbac_base -- sync the 'rules:' block and commit the result" >&2
	exit 1
fi

echo "==> No drift found"

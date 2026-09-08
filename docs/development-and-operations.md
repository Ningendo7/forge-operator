# Development

```sh
make test             # unit + envtest integration tests
make test-e2e         # real kind cluster, full Application lifecycle (see below)
make test-integration # real AWS/Akamai API calls against a live account (see below)
make lint             # golangci-lint
make run              # run the controller locally against your current kubeconfig
make generate         # regenerate deepcopy code
make manifests        # regenerate CRDs/RBAC from kubebuilder markers
make docker-build IMG=<image>
```

`make lint` runs the full `golangci-lint` analysis, which can be heavy on a full first run (it builds its own cache) — CI runs it on every push, so it's fine to skip locally and let [`lint.yml`](../.github/workflows/lint.yml) catch anything before you tag a release.

## End-to-end tests

[test/e2e/e2e_test.go](../test/e2e/e2e_test.go) runs the actual built image against a real kind cluster (via `make test-e2e`, and automatically in CI on every push/PR) and covers, against the **real deployed RBAC** — not a permissive test client:

- creation → a real pod actually reaching `Running`/`Ready`
- the real Kubernetes disruption controller blocking an eviction once the PDB has no spare budget
- scaling `spec.replicas` and watching the Deployment actually converge
- a real storage misconfiguration surfacing as `Degraded` (not a crash-loop), and recovering to `Ready` once fixed
- the real deployed admission webhook rejecting an invalid Akamai storage config before it ever reaches the reconciler
- real garbage collection of owned resources on delete

## Live cloud integration tests

[internal/controller/s3/integration_test.go](../internal/controller/s3/integration_test.go) and [internal/controller/Akamai-Obj-Str/integration_test.go](../internal/controller/Akamai-Obj-Str/integration_test.go) (`//go:build integration`, run via `make test-integration`) are the one place real AWS/Akamai API calls actually get exercised — bucket lifecycle, ownership rejection, and adoption behavior against real accounts, not mocks or `client/fake`. They're excluded from `go test ./...` and CI entirely (the build tag), and each test skips itself (rather than failing) if no live credentials are reachable via the standard AWS/Linode credential chain — the same chain `NewManager` itself uses, so it transparently reuses whatever already authenticates your AWS CLI/Terraform in the shell. Every test creates its own uniquely-named bucket (and IAM role/access key) and tears it down via `t.Cleanup`, which still runs if the test body fails partway through. Run explicitly:

```sh
make test-integration
```

# Security notes

- The operator never persists raw object storage credentials on `Application.status` — status is broadly readable (anyone with `get`/`list` on `applications`) and stored in etcd as plaintext. Generated credentials live only in the operator-owned storage Secret. See the doc comment on `AkamaiStorageStatus` in [api/v1alpha1/application_types.go](../api/v1alpha1/application_types.go).
- The Akamai API token Secret (your input) and the operator's generated credentials Secret (its output) use distinct default names by design — see [Akamai Object Storage Flow](authentication-and-storage.md#akamai-object-storage-flow).
- Pod and container `securityContext` default to a restricted profile (`runAsNonRoot`, dropped capabilities, `RuntimeDefault` seccomp) unless overridden in the `Application` spec.
- `--metrics-secure=true` and HTTP/2 disabled by default in the manager itself (mitigates the HTTP/2 Rapid Reset class of CVEs).
- Bucket ownership is verified before the operator will manage (and potentially delete) an existing bucket — see [Ownership verification](authentication-and-storage.md#ownership-verification).
- The manager's informer cache for `Secret` objects is scoped to only the Secrets this operator itself creates and labels — not every Secret cluster-wide — with reads of user-supplied credential Secrets (`spec.storage.secretName`, `spec.storage.akamai.accessKeySecretRef`) bypassing the cache and going straight to the API server. See [Observability](observability.md#reduced-secret-cache-footprint).

# Current limitations

Worth knowing before you rely on this in production:

- The CRD is `v1alpha1` — the Kubernetes API conventions make no compatibility promises at this version; a future field rename/removal is possible without a formal deprecation cycle.
- E2E coverage (`make test-e2e`, runs in CI) exercises the no-op storage path and real cluster mechanics (RBAC, PDB, GC, HPA handoff); real AWS/Akamai API calls are only exercised by `make test-integration` (see [Live cloud integration tests](#live-cloud-integration-tests) above), which is **not** wired into CI — it needs live cloud credentials, which CI doesn't have.
- Recovering automatic ownership claim of a bucket this operator itself created, after a transient failure writing the ownership tag/marker, only works within a 1-hour window (`bucketCreationClaimWindow`) of that creation — see [Ownership verification](authentication-and-storage.md#ownership-verification). Past that window the bucket is treated as foreign and reclaiming it needs the manual adopt-bucket annotation.
- The [admission webhook](architecture.md#webhooks) covers storage misconfiguration and provider immutability for both AWS and Akamai; field-combination validation not requiring live cluster state is instead CRD-level CEL rules (`+kubebuilder:validation:XValidation`), also covering PDB, autoscaling, and ServiceAccount constraints.
- Installing the chart with its defaults requires cert-manager already present in the cluster — [`scripts/bootstrap-cluster.sh`](../scripts/bootstrap-cluster.sh) handles that as a one-time step after `terraform apply`, but it's a separate manual step, not something Terraform or the chart does for you automatically.
- Endpoint format, and AWS region validity specifically, are deliberately *not* validated at admission — an invalid region fails fast and clearly on the first real AWS API call, and a client-side region allowlist would need constant upkeep against AWS's own region rollout, which the AWS API itself already handles correctly.

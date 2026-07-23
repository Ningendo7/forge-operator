# forge-operator

forge-operator is a Kubebuilder-based Kubernetes operator that reconciles application runtime infrastructure from a single custom resource.

Repository status: active development.

## Current Snapshot

As of 2026-07-23, this repository contains a functional reconciliation architecture with ongoing refactors in API/storage layers and Terraform expansion.

Primary API source:

- [api/v1alpha1/application_types.go](api/v1alpha1/application_types.go)

Primary controller entrypoint:

- [internal/controller/application_controller.go](internal/controller/application_controller.go)

## What The Operator Reconciles

From an Application resource, the controller attempts to reconcile:

- Deployment
- Service
- ConfigMap
- Secret
- Ingress (optional)
- HorizontalPodAutoscaler (optional)
- PodDisruptionBudget (optional)
- Storage resources and storage Secret (provider dependent)

Current reconciliation sequencing is defined in:

- [internal/controller/desiredstate.go](internal/controller/desiredstate.go)

## API Surface

The Application spec currently models:

- Workload: image, replicas, resources, env
- Container wiring: port, config and secret mount references
- Networking: service and ingress configuration
- Reliability: autoscaling and PDB configuration
- Storage: provider, bucket, endpoint/region, secret references, provider-specific blocks

Sample CR:

- [config/samples/forge_v1alpha1_application.yaml](config/samples/forge_v1alpha1_application.yaml)

## Implementation Matrix

Operator components:

- Reconciler framework and finalizer handling: implemented
- Status management helpers: implemented in status package
- AWS-oriented storage manager path under s3 controller package: present
- Akamai object storage controller path: scaffold directory exists, implementation pending

Terraform components under [Terraform/AWS](Terraform/AWS):

- modules/vpc: implemented baseline
- modules/networking: implemented baseline
- modules/iam: implemented baseline
- modules/eks: implemented baseline, includes addon resources
- modules/irsa: implemented
- modules/monitoring: pending implementation
- environments/dev: active composition
- environments/prod: not yet populated

## Known Active Gaps

This branch currently includes in-progress code that may not pass a full Go build/test run until refactor work is completed.

Observed at repository level:

- Generated deepcopy output and API structs are not fully synchronized
- New storage code requires additional Go module dependencies in go.mod
- Some API/provider naming and status struct paths are in transition

If you are consuming this repository externally, treat the operator and Terraform as release-candidate quality only after CI is green on your target branch.

## Development Workflow

Prerequisites:

- Go 1.24+
- Docker
- kubectl
- Access to a Kubernetes cluster

Common targets:

```sh
make manifests
make generate
make test
make build
make run
```

Container image build and push:

```sh
make docker-build docker-push IMG=<registry>/forge-operator:<tag>
```

Install and deploy controller:

```sh
make install
make deploy IMG=<registry>/forge-operator:<tag>
```

Apply sample:

```sh
kubectl apply -k config/samples/
```

Cleanup:

```sh
kubectl delete -k config/samples/
make undeploy
make uninstall
```

## Contributing

Before opening a PR:

- Keep generated artifacts current (manifests/deepcopy)
- Run format, vet, and tests locally
- Document any intentionally deferred work in code comments or PR notes

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.


# forge-operator

forge-operator is a Kubernetes operator built with Kubebuilder that manages application runtime resources from a single Application custom resource.

This repository is in active development. Core reconciliation for application workloads is implemented. Parts of the Terraform infrastructure stack are still in progress.

## Scope

For each Application resource, the controller reconciles:

- Deployment
- Service
- ConfigMap
- Secret
- Ingress (optional)
- HorizontalPodAutoscaler (optional)
- PodDisruptionBudget (optional)
- Storage credential Secret (optional, provider-backed configuration)

## Status

Implemented now:

- Application CRD with validation/default markers
- Controller finalizer flow
- Reconciliation for Deployment, Service, ConfigMap, Secret, Ingress, HPA, and PDB
- Storage configuration handling through Secrets
- Sample manifest covering full spec surface

Still in progress:

- Terraform IRSA module under Terraform/AWS/modules/irsa
- Terraform monitoring module under Terraform/AWS/modules/monitoring
- End-to-end infrastructure integration hardening across environments

## Application CRD

The API is defined in [api/v1alpha1/application_types.go](api/v1alpha1/application_types.go).

High-level spec areas:

- Workload: image, replicas, resources, environment variables
- Container wiring: container port, config and secret mount references
- Networking: service and optional ingress (including annotations and TLS)
- Reliability: optional autoscaling and pod disruption budget
- Storage: optional object storage provider configuration

Sample resource:

- [config/samples/forge_v1alpha1_application.yaml](config/samples/forge_v1alpha1_application.yaml)

## Local Development

Prerequisites:

- Go 1.24+
- Docker
- kubectl
- Access to a Kubernetes cluster (or local test cluster)

Generate CRDs and code:

```sh
make manifests
make generate
```

Run unit tests:

```sh
make test
```

Run controller locally:

```sh
make run
```

## Build and Deploy

Build and push image:

```sh
make docker-build docker-push IMG=<registry>/forge-operator:<tag>
```

Install CRDs and deploy controller:

```sh
make install
make deploy IMG=<registry>/forge-operator:<tag>
```

Apply sample Application:

```sh
kubectl apply -k config/samples/
```

Cleanup:

```sh
kubectl delete -k config/samples/
make undeploy
make uninstall
```

## Terraform

Terraform layout is under Terraform/AWS:

- modules/vpc, modules/networking, modules/iam, modules/eks are active
- modules/irsa and modules/monitoring are placeholders in progress
- environments/dev contains environment composition files

Until IRSA and monitoring are completed, treat Terraform as a working baseline and validate plans per environment before production rollout.

## Contributing

When changing API types, always regenerate manifests and deepcopy code before committing.

Before opening a PR:

- Run make manifests
- Run make generate
- Run make test

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


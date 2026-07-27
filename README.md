# forge-operator

forge-operator is a Kubebuilder-based Kubernetes operator for managing application runtime resources from a single Application custom resource.

Repository status: active development.

## Overview

The controller reconciles the following resource types from `spec`:

- ServiceAccount
- ConfigMap
- Secret
- Service
- Deployment
- Ingress
- PodDisruptionBudget
- HorizontalPodAutoscaler
- Storage resources and storage Secret

Reconciliation order is defined in [internal/controller/desiredstate.go](internal/controller/desiredstate.go).

## API Surface

The Application API is defined in [api/v1alpha1/application_types.go](api/v1alpha1/application_types.go).

Current spec areas include:

- image and replicas
- container port, mount paths, and volume wiring
- ConfigMap and Secret references
- Service type, port, and targetPort
- optional ingress host, path, class name, annotations, and TLS
- optional autoscaling limits and CPU target
- optional PodDisruptionBudget settings
- optional object storage configuration for AWS or Akamai
- optional ServiceAccount configuration
- environment variables and resource requests/limits

The Application status includes conditions, observed generation, and storage status.

## Controller Behavior

The controller entrypoint is [internal/controller/application_controller.go](internal/controller/application_controller.go).

It uses a finalizer for cleanup, then updates status through the status manager with Ready, Progressing, and Degraded conditions.

Supporting logic lives in:

- [internal/controller/finalizer.go](internal/controller/finalizer.go)
- [internal/controller/status](internal/controller/status)
- [internal/controller/desiredstate.go](internal/controller/desiredstate.go)

The manager entrypoint in [cmd/main.go](cmd/main.go) also reads `OIDC_PROVIDER_ARN` and `OIDC_PROVIDER_URL` from the environment for IRSA-related setup.

## Storage

Storage support is split between the API and controller packages:

- AWS storage flows live under [internal/controller/s3](internal/controller/s3)
- Akamai object storage has API support and a placeholder controller path under [internal/controller/Akamai-Obj-Str](internal/controller/Akamai-Obj-Str)
- Storage status is surfaced on the Application status object

## Terraform Layout

Terraform lives under [Terraform/AWS](Terraform/AWS).

Current environment and module layout:

- modules/vpc
- modules/networking
- modules/iam
- modules/eks
- modules/irsa
- modules/monitoring
- environments/dev
- environments/prod

Current state of those folders:

- dev contains the active composition
- prod exists but is empty
- monitoring is empty
- irsa contains implementation

The dev composition wires VPC, networking, IAM, EKS, and IRSA together and includes the IAM policy used by the operator for S3 and IRSA permissions.

## Deployment Workflow

Prerequisites:

- Go 1.24+
- Docker
- kubectl
- Access to a Kubernetes cluster

Build and verify locally:

```sh
make manifests
make generate
make test
make build
```

Run the controller locally:

```sh
make run
```

Build and publish a container image:

```sh
make docker-build docker-push IMG=<registry>/forge-operator:<tag>
```

Install and deploy the controller:

```sh
make install
make deploy IMG=<registry>/forge-operator:<tag>
```

Apply the sample resource:

```sh
kubectl apply -k config/samples/
```

Clean up:

```sh
kubectl delete -k config/samples/
make undeploy
make uninstall
```

Sample manifest:

- [config/samples/forge_v1alpha1_application.yaml](config/samples/forge_v1alpha1_application.yaml)

## Operational Notes

This repository is still under active refactor. Treat the README as a current implementation guide, not a release contract.

Before promoting a branch, verify the Go build, controller tests, and Terraform plan in your target environment.

## Contributing

Before opening a PR:

- Keep generated manifests and deepcopy code up to date
- Run format, vet, and tests locally
- Update the README if behavior or module wiring changes

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


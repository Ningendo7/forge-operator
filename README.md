# forge-operator

forge-operator is a Kubebuilder-based Kubernetes operator for managing application workloads through a single Application custom resource. It reconciles a Deployment, Service, ConfigMap, Secret, optional Ingress, optional PodDisruptionBudget, and optional HorizontalPodAutoscaler based on the spec you declare.

## What it manages

The operator is designed to support a production-like deployment workflow for a containerized application with:

- a Deployment driven by the Application image and replica count
- a Service exposing the container port
- a ConfigMap and Secret mounted into the pod
- optional ingress routing
- optional autoscaling and disruption budgets
- optional storage credentials for S3 or Akamai-style object storage

## CRD shape at a glance

The Application spec currently supports:

- image, replicas, and resources
- container port and mount paths
- service type/port/targetPort
- ingress host, path, class, and annotations
- autoscaling min/max replicas and CPU target
- PDB minAvailable/maxUnavailable
- storage provider, bucket, region, endpoint, and secretName
- config and secret data blocks

## Getting started

### Prerequisites

- Go 1.24+
- Docker
- kubectl
- a reachable Kubernetes cluster

### Build and deploy

1. Build and push the image:

```sh
make docker-build docker-push IMG=<registry>/forge-operator:tag
```

2. Install the CRDs:

```sh
make install
```

3. Deploy the controller:

```sh
make deploy IMG=<registry>/forge-operator:tag
```

4. Apply the sample:

```sh
kubectl apply -k config/samples/
```

### Example Application manifest

A representative manifest is provided in [config/samples/forge_v1alpha1_application.yaml](config/samples/forge_v1alpha1_application.yaml).

### Cleanup

```sh
kubectl delete -k config/samples/
make uninstall
make undeploy
```

## Sample configuration notes

- The operator uses the Application name as the default owner and naming basis for generated resources.
- If you set storage config, the operator will reconcile a Secret containing storage connection data for the application.
- If you supply ingress settings, a Kubernetes Ingress resource is created for the application service.
- If you supply autoscaling settings, an HPA is created targeting the generated Deployment.

## Development

Regenerate manifests and deepcopy code after changing API types:

```sh
make manifests generate
```

Run the controller tests:

```sh
go test ./internal/controller/...
```

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


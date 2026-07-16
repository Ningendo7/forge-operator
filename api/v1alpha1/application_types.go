/*
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
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ApplicationSpec defines the desired state of Application
type ApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// foo is an example field of Application. Edit application_types.go to remove/update
	// +optional
	Foo *string `json:"foo,omitempty"`
	// Container image to deploy.
	Image string `json:"image"`

	// Number of replicas.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Container configuration.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`

	// Kubernetes Service configuration.
	// +optional
	Service ServiceSpec `json:"service,omitempty"`

	// Ingress configuration.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Horizontal Pod Autoscaler configuration.
	// +optional
	Autoscaling *AutoscalingSpec `json:"autoscaling,omitempty"`

	// Object storage configuration.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// Environment variables.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Resource requests and limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ApplicationStatus defines the observed state of Application.
type ApplicationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the Application resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ContainerSpec defines container settings.
type ContainerSpec struct {
	// Container port.
	// +optional
	Port int32 `json:"port,omitempty"`
}

// ServiceSpec defines service settings.
type ServiceSpec struct {
	// Service type.
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`

	// Service port.
	Port int32 `json:"port"`
}

// IngressSpec defines ingress settings.
type IngressSpec struct {
	// Hostname for ingress.
	Host string `json:"host"`
}

// AutoscalingSpec defines HPA settings.
type AutoscalingSpec struct {
	// Minimum replicas.
	MinReplicas int32 `json:"minReplicas"`

	// Maximum replicas.
	MaxReplicas int32 `json:"maxReplicas"`

	// Target CPU utilization percentage.
	// +optional
	CPUUtilization *int32 `json:"cpuUtilization,omitempty"`
}

// StorageSpec defines object storage settings.
type StorageSpec struct {
	// Storage provider: aws or akamai.
	Provider string `json:"provider"`

	// Bucket name.
	Bucket string `json:"bucket"`

	// Cloud region.
	// +optional
	Region string `json:"region,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Application is the Schema for the applications API
type Application struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Application
	// +required
	Spec ApplicationSpec `json:"spec"`

	// status defines the observed state of Application
	// +optional
	Status ApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApplicationList contains a list of Application
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Application{}, &ApplicationList{})
		return nil
	})
}

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
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ApplicationSpec defines the desired state of Application
type ApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Container image to deploy.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// Number of replicas.
	// +optional
	// +kubebuilder:default:=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Container configuration.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`

	// ConfigMap configuration.
	// +optional
	Config *ConfigSpec `json:"config,omitempty"`

	// Secret configuration.
	// +optional
	Secret *SecretSpec `json:"secret,omitempty"`

	// Kubernetes Service configuration.
	// +optional
	Service ServiceSpec `json:"service,omitempty"`

	// Ingress configuration.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Horizontal Pod Autoscaler configuration.
	// +optional
	Autoscaling *AutoscalingSpec `json:"autoscaling,omitempty"`

	// Pod Disruption Budget configuration.
	// +optional
	PDB *PDBSpec `json:"pdb,omitempty"`

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
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=8080
	Port int32 `json:"port,omitempty"`

	// ConfigMap name to mount as configuration.
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// Secret name to mount as secrets.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// Mount path for the config volume.
	// +optional
	ConfigMountPath string `json:"configMountPath,omitempty"`

	// Mount path for the secret volume.
	// +optional
	SecretMountPath string `json:"secretMountPath,omitempty"`
}

// ConfigSpec defines the ConfigMap data that the operator manages.
type ConfigSpec struct {
	// Name of the ConfigMap to reconcile.
	// +optional
	Name string `json:"name,omitempty"`

	// Data stored in the ConfigMap.
	// +optional
	Data map[string]string `json:"data,omitempty"`
}

// SecretSpec defines the Secret data that the operator manages.
type SecretSpec struct {
	// Name of the Secret to reconcile.
	// +optional
	Name string `json:"name,omitempty"`

	// String data stored in the Secret.
	// +optional
	StringData map[string]string `json:"stringData,omitempty"`

	// Secret type.
	// +optional
	Type corev1.SecretType `json:"type,omitempty"`
}

// ServiceSpec defines service settings.
type ServiceSpec struct {
	// Service type.
	// +optional
	// +kubebuilder:default:=ClusterIP
	Type corev1.ServiceType `json:"type,omitempty"`

	// Service port exposed to clients.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=80
	Port int32 `json:"port,omitempty"`

	// TargetPort is the port the application container listens on.
	// When omitted, it defaults to the container port from spec.container.port.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TargetPort *int32 `json:"targetPort,omitempty"`
}

// IngressSpec defines ingress settings.
type IngressSpec struct {
	// Hostname for ingress.
	// +optional
	Host string `json:"host,omitempty"`

	// Path for the ingress rule.
	// +optional
	Path string `json:"path,omitempty"`

	// PathType for the ingress rule.
	// +optional
	PathType *networkingv1.PathType `json:"pathType,omitempty"`

	// ClassName for the ingress controller.
	// +optional
	ClassName *string `json:"className,omitempty"`

	// Annotations for the ingress resource.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PDBSpec defines Pod Disruption Budget settings.
type PDBSpec struct {
	// Minimum available pods.
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// Maximum unavailable pods.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// AutoscalingSpec defines HPA settings.
type AutoscalingSpec struct {
	// Minimum replicas.
	// +kubebuilder:validation:Minimum=1
	MinReplicas int32 `json:"minReplicas"`

	// Maximum replicas.
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// Target CPU utilization percentage.
	// +optional
	CPUUtilization *int32 `json:"cpuUtilization,omitempty"`
}

// StorageSpec defines object storage settings.
type StorageSpec struct {
	// Storage provider: aws-s3 or akamai-object-storage.
	// +kubebuilder:validation:Enum=aws-s3;akamai-object-storage
	Provider string `json:"provider"`

	// Bucket name.
	Bucket string `json:"bucket"`

	// Cloud region.
	// +optional
	Region string `json:"region,omitempty"`

	// Endpoint for the object storage service.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Secret name containing access credentials.
	// +optional
	SecretName string `json:"secretName,omitempty"`
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

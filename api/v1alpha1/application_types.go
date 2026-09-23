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

// StorageProvider represents the supported object storage backends.
//
//	+kubebuilder:validation:Enum=AWS;Akamai
type StorageProvider string

// DeletionPolicy controls what happens to the provisioned cloud storage
// bucket when this Application is deleted.
// +kubebuilder:validation:Enum=Delete;Retain
type DeletionPolicy string

const (
	// DeletionPolicyDelete deletes the bucket (and its contents) along with
	// the Application.
	DeletionPolicyDelete DeletionPolicy = "Delete"

	// DeletionPolicyRetain (the default) removes the Application and its
	// finalizer but leaves the bucket and its ownership marker in place.
	DeletionPolicyRetain DeletionPolicy = "Retain"
)

const (
	ProviderAWSS3               StorageProvider = "AWS"
	ProviderAkamaiObjectStorage StorageProvider = "Akamai"
)

// ApplicationSpec defines the desired state of Application
// +kubebuilder:validation:XValidation:rule="!(has(self.autoscaling) && has(self.autoscaling.cpuUtilization) && !('cpu' in self.resources.requests))",message="spec.resources.requests.cpu must be set when spec.autoscaling.cpuUtilization is set -- the HPA can't compute utilization without a CPU request to divide by"
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
	ConfigMap *ConfigSpec `json:"config,omitempty"`

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

	ServiceAccount *ServiceAccountSpec `json:"serviceAccount,omitempty"`
	Env            []corev1.EnvVar     `json:"env,omitempty"`

	// Resource requests and limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Pod-level security context. Defaults to a restricted profile if unset.
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
}

// ApplicationStatus defines the observed state of Application.
type ApplicationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Conditions reflect current state: Available, Progressing, Degraded.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Storage represents the status of the requested object storage resources.
	// +optional
	Storage *StorageStatus `json:"storage,omitempty"`
}

// StorageStatus defines the observed state of the object storage backend.
type StorageStatus struct {
	// Provider indicates the active storage provider being used.
	// +optional
	Provider StorageProvider `json:"provider,omitempty"`

	// Bucket is the name of the provisioned bucket.
	// +optional
	Bucket string `json:"bucket,omitempty"`

	// Created records whether this operator itself successfully created
	// this bucket, as opposed to finding one that already existed.
	// Internal bookkeeping: combined with CreatedAt, it lets a later
	// reconcile recover from a transient failure tagging/marking a bucket
	// it just created, without treating every untagged bucket as
	// automatically its own.
	// +optional
	Created bool `json:"created,omitempty"`

	// CreatedAt records when Created was set, bounding how long it's
	// trusted as ownership provenance (see bucketCreationClaimWindow) --
	// a deleted bucket's name can be reused by something unrelated, so
	// this can't be a permanent claim.
	// +optional
	CreatedAt metav1.Time `json:"createdAt,omitempty"`

	// Region is the cloud region where the bucket was provisioned.
	// +optional
	Region string `json:"region,omitempty"`

	// SecretName is the Secret spec.storage.secretName pointed at when this
	// bucket was last successfully reconciled, if any. Recorded so cleanup
	// can still authenticate to the cloud provider after spec.storage has
	// been removed from the Application -- at that point this is the only
	// remaining record of which credentials Secret to use.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// DeletionPolicy is spec.storage.deletionPolicy as it stood when this
	// bucket was last successfully reconciled. Recorded for the same reason
	// as SecretName: once spec.storage is removed, this is the only
	// remaining record of whether the bucket should actually be deleted or
	// left in place during the cleanup that removal triggers.
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// AWS contains AWS-specific status information.
	// +optional
	AWS *AWSStorageStatus `json:"aws,omitempty"`

	// Akamai contains Akamai-specific status information.
	// +optional
	Akamai *AkamaiStorageStatus `json:"akamai,omitempty"`
}

// AWSStorageStatus defines AWS-specific status outputs.
type AWSStorageStatus struct {
	// RoleARN is the IAM Role ARN generated for IRSA.
	// +optional
	RoleARN string `json:"roleARN,omitempty"`
}

// AkamaiStorageStatus defines Akamai Object Storage status outputs.
//
// Credentials are deliberately not exposed here: status is persisted as
// plaintext in etcd and readable by anyone with get/list on applications, a
// much broader audience than whoever has get on secrets. The access/secret
// key pair is written only to the storage Secret (see Secret.go's
// desiredStorage); status.storage.akamai never mirrors it.
type AkamaiStorageStatus struct {
	// Endpoint is the active S3-compatible host endpoint generated for the bucket.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// AccessKeySecretRef is spec.storage.akamai.accessKeySecretRef as it
	// stood when this bucket was last successfully reconciled, if any --
	// same reasoning as StorageStatus.SecretName: once spec.storage is
	// removed, this is the only remaining record of which Secret holds
	// the Akamai API token cleanup needs. Only the Secret name is
	// recorded, never its contents.
	// +optional
	AccessKeySecretRef string `json:"accessKeySecretRef,omitempty"`
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

	// Container-level security context. Defaults to a restricted profile if unset.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// Liveness probe for the container.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// Readiness probe for the container.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// Startup probe for the container.
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`
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
	// +kubebuilder:default:=Prefix
	PathType *networkingv1.PathType `json:"pathType,omitempty"`

	// ClassName for the ingress controller.
	// +optional
	ClassName *string `json:"className,omitempty"`

	// Annotations for the ingress resource.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS configuration for the ingress resource.
	// +optional
	TLS []networkingv1.IngressTLS `json:"tls,omitempty"`
}

// PDBSpec defines Pod Disruption Budget settings.
// +kubebuilder:validation:XValidation:rule="!(has(self.minAvailable) && has(self.maxUnavailable))",message="cannot set both minAvailable and maxUnavailable"
type PDBSpec struct {
	// Minimum available pods.
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// Maximum unavailable pods.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// AutoscalingSpec defines HPA settings.
// +kubebuilder:validation:XValidation:rule="self.maxReplicas >= self.minReplicas",message="maxReplicas must be greater than or equal to minReplicas"
type AutoscalingSpec struct {
	// Minimum replicas.
	// +kubebuilder:validation:Minimum=1
	MinReplicas int32 `json:"minReplicas"`

	// Maximum replicas.
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// Target CPU utilization percentage.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	CPUUtilization *int32 `json:"cpuUtilization,omitempty"`
}

// ServiceAccountSpec defines ServiceAccount behavior.
// +kubebuilder:validation:XValidation:rule="!(has(self.create) && !self.create && self.name.size() == 0)",message="create: false requires name to be set -- there's no existing ServiceAccount to point at otherwise"
type ServiceAccountSpec struct {
	// Name of an existing ServiceAccount to use.
	// if empty and create is true, a new ServiceAccount will be created.
	// +optional
	Name string `json:"name,omitempty"`
	// Create a new ServiceAccount if it doesn't exist.
	// Defaults to true
	Create *bool `json:"create,omitempty"`
}

// StorageSpec defines object storage settings.
// +kubebuilder:validation:XValidation:rule="!(self.provider == 'AWS' && has(self.akamai))",message="spec.storage.akamai must not be set when provider is AWS"
// +kubebuilder:validation:XValidation:rule="!(self.provider == 'Akamai' && has(self.aws))",message="spec.storage.aws must not be set when provider is Akamai"
type StorageSpec struct {
	// Storage provider: AWS S3 or Akamai Object Storage.
	Provider StorageProvider `json:"provider"`

	// Bucket name.
	Bucket string `json:"bucket"`

	// DeletionPolicy controls what happens to the bucket when this
	// Application is deleted. Defaults to "Retain": losing real data
	// because an Application was deleted is worse than a retained bucket
	// costing a few cents until someone notices. Set explicitly to
	// "Delete" for ephemeral Applications (dev sandboxes, PR previews)
	// where automatic cleanup is actually wanted.
	// +optional
	// +kubebuilder:default=Retain
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// Cloud region. For AWS, a standard AWS region (e.g. "us-east-1"). For
	// Akamai, a modern Linode region slug (e.g. "us-iad", "us-mia") -- the
	// same naming used by LKE, not the older "-1"-suffixed cluster form
	// (e.g. "us-iad-1"), which is deprecated on Linode's side.
	// +optional
	Region string `json:"region,omitempty"`

	// Endpoint for the object storage service.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Secret name containing access credentials.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// AWS-specific configuration (populated only if Provider == AWS).
	AWS *AWSStorageSpec `json:"aws,omitempty"`

	// Akamai-specific configuration (populated only if Provider == Akamai).
	Akamai *AkamaiStorageSpec `json:"akamai,omitempty"`
}

// AWSStorageSpec defines AWS-specific storage configuration.
type AWSStorageSpec struct {

	// VersioningEnabled controls whether S3 bucket versioning is enabled.
	// Defaults to true when unset, matching this operator's previous
	// hardcoded behavior. Explicitly setting this to false suspends
	// versioning -- S3 has no way to fully un-version a bucket once
	// versioning has ever been enabled on it, only enable/suspend.
	// +optional
	VersioningEnabled *bool `json:"versioningEnabled,omitempty"`

	// LifecycleRules configures S3 Object Lifecycle rules for the bucket.
	//
	// When this field is left entirely unset, a single default rule is
	// applied, matching this operator's previous hardcoded behavior: abort
	// incomplete multipart uploads after 7 days, expire noncurrent object
	// versions after 30 days, and transition current objects to
	// STANDARD_IA after 30 days.
	//
	// Set this to an explicit empty list (`lifecycleRules: []`) to remove
	// lifecycle policy from the bucket entirely, or provide one or more
	// rules of your own to fully replace the default.
	// +optional
	LifecycleRules []LifecycleRule `json:"lifecycleRules,omitempty"`
}

// LifecycleRule configures a single S3 Object Lifecycle rule. At least one
// of ExpirationDays, NoncurrentVersionExpirationDays,
// AbortIncompleteMultipartUploadDays, or Transitions should be set, or the
// rule has no effect.
type LifecycleRule struct {
	// ID uniquely identifies this rule within the bucket's lifecycle
	// configuration. Defaults to "rule-<index>" (its position in the list)
	// if unset.
	// +optional
	ID string `json:"id,omitempty"`

	// Enabled controls whether this rule is currently applied. Defaults to
	// true.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Prefix limits this rule to object keys starting with this prefix.
	// Applies to every object in the bucket when unset.
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// ExpirationDays permanently deletes objects this many days after
	// creation.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ExpirationDays *int32 `json:"expirationDays,omitempty"`

	// NoncurrentVersionExpirationDays deletes noncurrent (overwritten or
	// deleted) object versions this many days after they became
	// noncurrent. Only meaningful when versioningEnabled is true.
	// +optional
	// +kubebuilder:validation:Minimum=1
	NoncurrentVersionExpirationDays *int32 `json:"noncurrentVersionExpirationDays,omitempty"`

	// AbortIncompleteMultipartUploadDays aborts incomplete multipart
	// uploads this many days after they were initiated.
	// +optional
	// +kubebuilder:validation:Minimum=1
	AbortIncompleteMultipartUploadDays *int32 `json:"abortIncompleteMultipartUploadDays,omitempty"`

	// Transitions moves objects to a different storage class after a
	// number of days.
	// +optional
	Transitions []LifecycleTransition `json:"transitions,omitempty"`
}

// LifecycleTransition moves objects to a different S3 storage class after a
// number of days from object creation.
type LifecycleTransition struct {
	// Days after object creation to transition.
	// +kubebuilder:validation:Minimum=1
	Days int32 `json:"days"`

	// StorageClass is the target S3 storage class.
	// +kubebuilder:validation:Enum=STANDARD_IA;ONEZONE_IA;INTELLIGENT_TIERING;GLACIER;DEEP_ARCHIVE;GLACIER_IR
	StorageClass string `json:"storageClass"`
}

// AkamaiStorageSpec defines Akamai-specific storage configuration.
type AkamaiStorageSpec struct {
	// Name of the Secret (key: apiToken) holding the Akamai/Linode API token
	// used to authenticate to the Object Storage API. Defaults to
	// "<application-name>-akamai-token" if unset. This must name a Secret
	// distinct from spec.storage.secretName, which is the operator's own
	// generated output credentials Secret (bucket access/secret key) — the
	// two are not interchangeable.
	// +optional
	AccessKeySecretRef string `json:"accessKeySecretRef,omitempty"`

	// Optional endpoint override
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// InjectCredentials adds this bucket's access key/secret key/endpoint/
	// region/bucket name to the Application's own container as environment
	// variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_ENDPOINT_URL,
	// AWS_REGION, FORGE_STORAGE_BUCKET). Defaults to false. Akamai-only:
	// AWS Applications already get equivalent access transparently through
	// IRSA, with no opt-in needed; Akamai has no IRSA-equivalent, so this
	// is the only way to make the bucket usable by the Application itself
	// rather than just administratively provisioned.
	// +optional
	InjectCredentials bool `json:"injectCredentials,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

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

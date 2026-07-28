package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/api/resource"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDesiredDeployment_DefaultReplicas(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app)

	if deployment.Name != "demo-app-deployment" {
		t.Fatalf("expected deployment name %q, got %q", 
		"demo-app", 
		deployment.Name)
	}

	if deployment.Namespace != "default" {
		t.Fatalf("expected deployment namespace %q, got %q", 
		"default", 
		deployment.Namespace)
	}

	if *deployment.Spec.Replicas != 1 {
		t.Fatalf("expected default replicas to be 1, got %d", *deployment.Spec.Replicas)
	}
}

func TestDesiredDeployment_ConfiguredReplicas(t *testing.T) {
	replicas := int32(3)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image:    "nginx:latest",
			Replicas: &replicas,
		},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app)

	if *deployment.Spec.Replicas != 3 {
		t.Fatalf("expected configured replicas to be 3, got %d", *deployment.Spec.Replicas)
	}
}

func TestDesiredDeployment_UsesApplicationImage(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:1.27",
		},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.Image != "nginx:1.27" {
		t.Fatalf("expected container image to be %q, got %q", "nginx:1.27", container.Image)
	}
}

func TestDesiredDeployment_DefaultContainerPort(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.Ports[0].ContainerPort != 8080 {
		t.Fatalf("expected default container port to be 8080, got %d", container.Ports[0].ContainerPort)
	}
}

func TestDesiredDeployment_ConfiguredContainerPort(t *testing.T) {
	port := int32(9090)
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Container: forgev1alpha1.ContainerSpec{
				Port: port,
			},
		},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.Ports[0].ContainerPort != 9090 {
		t.Fatalf("expected configured container port to be 9090, got %d", container.Ports[0].ContainerPort)
	}
}

func TestBuildVolumeandMounts_ConfigMap(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Container: forgev1alpha1.ContainerSpec{
				ConfigMapName: "custom-config",
			},
		},
	}

	r := &ApplicationReconciler{}
	volumes, volumeMounts := r.buildVolumeAndMounts(app)

	if len(volumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(volumeMounts))
	}

	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(volumes))
	}

	if volumes[0].ConfigMap.Name != "custom-config" {
		t.Fatalf("expected config map volume name to be %q, got %q", "custom-config", volumes[0].ConfigMap.Name)
	}
}

func TestBuildVolumeandMounts_Secret(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Container: forgev1alpha1.ContainerSpec{
				SecretName: "custom-secret",
			},
		},
	}

	r := &ApplicationReconciler{}
	volumes, volumeMounts := r.buildVolumeAndMounts(app)

	if len(volumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(volumeMounts))
	}

	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(volumes))
	}

	if volumes[0].Secret.SecretName != "custom-secret" {
		t.Fatalf("expected secret volume name to be %q, got %q", "custom-secret", volumes[0].Secret.SecretName)
	}
}

func TestBuildVolumeandMounts_DefaultConfigMountPath(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Container: forgev1alpha1.ContainerSpec{
				ConfigMapName: "custom-config",
			},
		},
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/etc/demo-app/config" {
		t.Fatalf("expected default config mount path to be %q, got %q", "/etc/demo-app/config", volumeMounts[0].MountPath)
	}
}

func TestBuildVolumeandMounts_CustomConfigMountPath(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Container: forgev1alpha1.ContainerSpec{
				ConfigMapName:    "custom-config",
				ConfigMountPath:  "/custom/config",
			},
		},
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/custom/config" {
		t.Fatalf("expected custom config mount path to be %q, got %q", "/custom/config", volumeMounts[0].MountPath)
	}
}

func TestBuildVolumeandMounts_DefaultSecretMountPath(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Container: forgev1alpha1.ContainerSpec{
				SecretName: "custom-secret",
			},
		},
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/etc/demo-app/secret" {
		t.Fatalf("expected default secret mount path to be %q, got %q", "/etc/demo-app/secret", volumeMounts[0].MountPath)
	}
}

func TestBuildVolumeandMounts_CustomSecretMountPath(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Container: forgev1alpha1.ContainerSpec{
				SecretName:      "custom-secret",
				SecretMountPath: "/custom/secret",
			},
		},
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/custom/secret" {
		t.Fatalf("expected custom secret mount path to be %q, got %q", "/custom/secret", volumeMounts[0].MountPath)
	}
}

func TestConfigMapNameFor(t *testing.T) {
	tests := []struct {
		name     string
		application string
		configMapName string
		expected string
	}{
		{
			name:     "uses config map name from spec",
			application: "demo-app",
			configMapName: "custom-config",
			expected: "custom-config",
		},
		{
			name:     "uses default config map name when not specified",
			application: "demo-app",
			configMapName: "",
			expected: "demo-app-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: tt.application, Namespace: "default"},
				Spec: forgev1alpha1.ApplicationSpec{
					Image: "nginx:latest",
					Container: forgev1alpha1.ContainerSpec{
						ConfigMapName: tt.configMapName,
					},
				},
			}

			result := configMapNameFor(app)
			if result != tt.expected {
				t.Fatalf("expected config map name to be %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSecretNameFor(t *testing.T) {
	tests := []struct {
		name     string
		application string
		secretName string
		expected string
	}{
		{
			name:     "uses secret name from spec",
			application: "demo-app",
			secretName: "custom-secret",
			expected: "custom-secret",
		},
		{
			name:     "uses default secret name when not specified",
			application: "demo-app",
			secretName: "",
			expected: "demo-app-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: tt.application, Namespace: "default"},
				Spec: forgev1alpha1.ApplicationSpec{
					Image: "nginx:latest",
					Container: forgev1alpha1.ContainerSpec{
						SecretName: tt.secretName,
					},
				},
			}

			result := secretNameFor(app)
			if result != tt.expected {
				t.Fatalf("expected secret name to be %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDesiredPodSpec_ServiceAccount(t *testing.T) {
	create := true
	doNotCreate := false

	tests := []struct {
		name     string
		serviceAccount *forgev1alpha1.ServiceAccountSpec
		expectedName string
	}{
		{
			name: "creates service account when not specified",
			serviceAccount: nil,
			expectedName: "demo-app-sa",
		},
		{
			name: "creates service account when create is true",
			serviceAccount : &forgev1alpha1.ServiceAccountSpec{
				Name: "custom-sa",
			},
			expectedName: "custom-sa",
		},
		{
			name: "does not create service account when create is false",
			serviceAccount : &forgev1alpha1.ServiceAccountSpec{
				Create: &doNotCreate,
			},
			expectedName: "",
		},
		{
			name: "creates service account when create is true and name is specified",
			serviceAccount : &forgev1alpha1.ServiceAccountSpec{
				Create: &create,
			},
			expectedName: "demo-app-sa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
				Spec: forgev1alpha1.ApplicationSpec{
					Image: "nginx:latest",
					ServiceAccount: tt.serviceAccount,
				},
			}

			r := &ApplicationReconciler{}
			podSpec := r.desiredPodSpec(app)

			if podSpec.ServiceAccountName != tt.expectedName {
				t.Fatalf("expected service account name to be %q, got %q", tt.expectedName, podSpec.ServiceAccountName)
			}
		})
	}
}

func TestReconcileDeployment_CreatesDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := r.reconcileDeployment(context.Background(), app)
	if err != nil {
		t.Fatalf("unexpected error during reconcileDeployment: %v", err)
	}

	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-deployment", Namespace: "default"}, deployment)
	if err != nil {
		t.Fatalf("expected deployment to be created, but got error: %v", err)
	}
}

func TestReconcileDeployment_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// First reconciliation
	err := r.reconcileDeployment(context.Background(), app)
	if err != nil {
		t.Fatalf("unexpected error during first reconcileDeployment: %v", err)
	}

	// Second reconciliation should be idempotent
	err = r.reconcileDeployment(context.Background(), app)
	if err != nil {
		t.Fatalf("unexpected error during second reconcileDeployment: %v", err)
	}

	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-deployment", Namespace: "default"}, deployment)
	if err != nil {
		t.Fatalf("expected deployment to exist, but got error: %v", err)
	}

	if deployment.Spec.Template.Spec.Containers[0].Image != "nginx:latest" {
		t.Fatalf("expected container image to be %q, got %q", "nginx:latest", deployment.Spec.Template.Spec.Containers[0].Image)
	}

	if *deployment.Spec.Replicas != 1 {
		t.Fatalf("expected replicas to be 1, got %d", *deployment.Spec.Replicas)
	}
}

func TestReconcileDeployment_UpdatesFields(t *testing.T) {
	tests := []struct {
		name          string
		Image  	     string
		replicas      int32
		expectedImage string
		expectedReplicas int32
	}{
		{
			name:          "updates replicas",
			Image:         "nginx:latest",
			replicas:      3,
			expectedImage: "nginx:latest",
			expectedReplicas: 3,
		},
		{
			name:          "updates image",
			Image:         "nginx:1.27",
			replicas:      1,
			expectedImage: "nginx:1.27",
			expectedReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = forgev1alpha1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
				Spec: forgev1alpha1.ApplicationSpec{
					Image: tt.Image,
				},
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			r := &ApplicationReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Initial reconcile
			err := r.reconcileDeployment(context.Background(), app)
			if err != nil {
				t.Fatalf("unexpected error during initial reconcileDeployment: %v", err)
			}

			// Change the desired state
			app.Spec.Image = tt.Image

			replicas := tt.replicas
			app.Spec.Replicas = &replicas

			// Reconcile again to apply changes
			err = r.reconcileDeployment(context.Background(), app)
			if err != nil {
				t.Fatalf("unexpected error during second reconcileDeployment: %v", err)
			}

			deployment := &appsv1.Deployment{}
			err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-deployment", Namespace: "default"}, deployment)
			if err != nil {
				t.Fatalf("expected deployment to exist, but got error: %v", err)
			}

			if *deployment.Spec.Replicas != tt.expectedReplicas {
				t.Fatalf("expected replicas to be %d, got %d", tt.expectedReplicas, *deployment.Spec.Replicas)
			}

			if deployment.Spec.Template.Spec.Containers[0].Image != tt.expectedImage {
				t.Fatalf("expected image to be %q, got %q", tt.expectedImage, deployment.Spec.Template.Spec.Containers[0].Image)
			}
		})
	}
}

func TestReconcileDeployment_SetsOwnerReference(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default", UID: "12345"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ApplicationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := r.reconcileDeployment(context.Background(), app)
	if err != nil {
		t.Fatalf("unexpected error during reconcileDeployment: %v", err)
	}

	deployment := &appsv1.Deployment{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-deployment", Namespace: "default"}, deployment)
	if err != nil {
		t.Fatalf("expected deployment to exist, but got error: %v", err)
	}

	if len(deployment.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(deployment.OwnerReferences))
	}

	owner := deployment.OwnerReferences[0]
	if owner.Name != app.Name {
		t.Fatalf("expected owner reference name %q, got %q", app.Name, owner.Name)
	}

	if owner.Kind != "Application" {
		t.Fatalf("expected owner reference kind %q, got %q", "Application", owner.Kind)
	}
}

func TestReconcileDeployment_UsesServiceAccount(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			ServiceAccount: &forgev1alpha1.ServiceAccountSpec{
				Name: "custom-sa",
			},
		},
	}

	r := &ApplicationReconciler{}

	deployment := r.desiredDeployment(app)
	serviceAccountName := deployment.Spec.Template.Spec.ServiceAccountName

	if serviceAccountName != "custom-sa" {
		t.Fatalf("expected service account name to be %q, got %q", "custom-sa", serviceAccountName)
	}
}

func TestReconcileDeployment_UsesResources(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("250m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app)
	resources := deployment.Spec.Template.Spec.Containers[0].Resources

	if resources.Requests.Cpu().String() != "250m" {
		t.Fatalf("expected CPU request to be %q, got %q", "250m", resources.Requests.Cpu().String())
	}

	if resources.Requests.Memory().String() != "256Mi" {
		t.Fatalf("expected memory request to be %q, got %q", "256Mi", resources.Requests.Memory().String())
	}

	if resources.Limits.Cpu().String() != "1" {
		t.Fatalf("expected CPU limit to be %q, got %q", "1", resources.Limits.Cpu().String())
	}

	if resources.Limits.Memory().String() != "1Gi" {
		t.Fatalf("expected memory limit to be %q, got %q", "1Gi", resources.Limits.Memory().String())
	}
}

// Unhappy path : Error Handling and Failure Scenarios
func TestReconcileDeployment_ReturnsErrorWhenDeploymentPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ApplicationReconciler{
		Client: &failingPatchClient{
			Client: fakeClient,
		},
		Scheme: scheme,
	}

	err := r.reconcileDeployment(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error during reconcileDeployment due to failing patch, but got none")
	}
}
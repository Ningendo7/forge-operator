package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

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

package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDesiredDeployment_DefaultReplicas(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)

	if deployment.Name != testDeploymentName {
		t.Fatalf("expected deployment name %q, got %q",
			testAppName,
			deployment.Name)
	}

	if deployment.Namespace != testNamespace {
		t.Fatalf("expected deployment namespace %q, got %q",
			testNamespace,
			deployment.Namespace)
	}

	if *deployment.Spec.Replicas != 1 {
		t.Fatalf("expected default replicas to be 1, got %d", *deployment.Spec.Replicas)
	}
}

func TestDesiredDeployment_ConfiguredReplicas(t *testing.T) {
	replicas := int32(3)

	app := newTestApplication()
	app.Spec.Replicas = &replicas

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)

	if *deployment.Spec.Replicas != 3 {
		t.Fatalf("expected configured replicas to be 3, got %d", *deployment.Spec.Replicas)
	}
}

func TestDesiredDeployment_HPAConfigured_SeedsReplicasOnFirstCreate(t *testing.T) {
	replicas := int32(2)
	app := newTestApplication()
	app.Spec.Replicas = &replicas
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 5}

	r := &ApplicationReconciler{}
	// deploymentExists=false: this is the initial creation, so spec.replicas
	// still seeds the starting count even though an HPA is configured.
	deployment := r.desiredDeployment(app, false)

	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Fatalf("expected initial replicas to be seeded from spec.replicas (2), got %v", deployment.Spec.Replicas)
	}
}

func TestDesiredDeployment_HPAConfigured_OmitsReplicasOnceDeploymentExists(t *testing.T) {
	replicas := int32(2)
	app := newTestApplication()
	app.Spec.Replicas = &replicas
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 5}

	r := &ApplicationReconciler{}
	// deploymentExists=true: the HPA may already own spec.replicas on the
	// live Deployment. Replicas must be nil here so the SSA apply omits the
	// field (it has an `omitempty` json tag) instead of force-overwriting
	// whatever the HPA last set.
	deployment := r.desiredDeployment(app, true)

	if deployment.Spec.Replicas != nil {
		t.Fatalf("expected replicas to be omitted once the HPA may own it, got %d", *deployment.Spec.Replicas)
	}
}

func TestDesiredDeployment_NoHPA_StillOwnsReplicasEvenIfDeploymentExists(t *testing.T) {
	replicas := int32(3)
	app := newTestApplication()
	app.Spec.Replicas = &replicas

	r := &ApplicationReconciler{}
	// No Autoscaling configured: the operator remains the sole owner of
	// replica count regardless of whether the Deployment already exists.
	deployment := r.desiredDeployment(app, true)

	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 3 {
		t.Fatalf("expected replicas to stay operator-owned at 3 with no HPA, got %v", deployment.Spec.Replicas)
	}
}

func TestDesiredDeployment_UsesApplicationImage(t *testing.T) {
	app := newTestApplication()
	app.Spec.Image = testImage127

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.Image != testImage127 {
		t.Fatalf("expected container image to be %q, got %q", testImage127, container.Image)
	}
}

func TestDesiredDeployment_UsesApplicationEnv(t *testing.T) {
	app := newTestApplication()
	app.Spec.Env = []corev1.EnvVar{
		{Name: "LOG_LEVEL", Value: "debug"},
		{Name: "PORT", Value: "9090"},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	container := deployment.Spec.Template.Spec.Containers[0]

	if len(container.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(container.Env))
	}
	if container.Env[0].Name != "LOG_LEVEL" || container.Env[0].Value != "debug" {
		t.Fatalf("expected LOG_LEVEL=debug, got %+v", container.Env[0])
	}
	if container.Env[1].Name != "PORT" || container.Env[1].Value != "9090" {
		t.Fatalf("expected PORT=9090, got %+v", container.Env[1])
	}
}

func TestDesiredDeployment_DefaultsToRestrictedSecurityContext(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	podSpec := deployment.Spec.Template.Spec
	container := podSpec.Containers[0]

	if podSpec.SecurityContext == nil || podSpec.SecurityContext.RunAsNonRoot == nil || !*podSpec.SecurityContext.RunAsNonRoot {
		t.Fatalf("expected default pod security context to set runAsNonRoot=true, got %#v", podSpec.SecurityContext)
	}
	if podSpec.SecurityContext.SeccompProfile == nil || podSpec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("expected default pod seccomp profile RuntimeDefault, got %#v", podSpec.SecurityContext.SeccompProfile)
	}

	if container.SecurityContext == nil {
		t.Fatalf("expected default container security context to be set")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("expected default allowPrivilegeEscalation=false, got %#v", container.SecurityContext.AllowPrivilegeEscalation)
	}
	if container.SecurityContext.Capabilities == nil || len(container.SecurityContext.Capabilities.Drop) != 1 || container.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("expected default capabilities.drop=[ALL], got %#v", container.SecurityContext.Capabilities)
	}
}

func TestDesiredDeployment_UsesConfiguredSecurityContext(t *testing.T) {
	app := newTestApplication()
	trueVal := true
	app.Spec.PodSecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: &trueVal}
	app.Spec.Container.SecurityContext = &corev1.SecurityContext{ReadOnlyRootFilesystem: &trueVal}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	podSpec := deployment.Spec.Template.Spec
	container := podSpec.Containers[0]

	if podSpec.SecurityContext != app.Spec.PodSecurityContext {
		t.Fatalf("expected configured pod security context to be used as-is")
	}
	if container.SecurityContext == nil || container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatalf("expected configured container security context to be used, got %#v", container.SecurityContext)
	}
}

func TestDesiredDeployment_UsesConfiguredProbes(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container.LivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(8080)}},
	}
	app.Spec.Container.ReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt(8080)}},
	}
	app.Spec.Container.StartupProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(8080)}},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.LivenessProbe == nil || container.LivenessProbe.HTTPGet.Path != "/healthz" {
		t.Fatalf("expected liveness probe to be set, got %#v", container.LivenessProbe)
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet.Path != "/readyz" {
		t.Fatalf("expected readiness probe to be set, got %#v", container.ReadinessProbe)
	}
	if container.StartupProbe == nil || container.StartupProbe.TCPSocket == nil {
		t.Fatalf("expected startup probe to be set, got %#v", container.StartupProbe)
	}
}

func TestDesiredDeployment_DefaultsToTCPProbesOnContainerPort(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.LivenessProbe == nil || container.LivenessProbe.TCPSocket == nil {
		t.Fatalf("expected default liveness probe to be a TCP check, got %#v", container.LivenessProbe)
	}
	if container.LivenessProbe.TCPSocket.Port.IntValue() != 8080 {
		t.Errorf("expected default liveness probe to target port 8080, got %d", container.LivenessProbe.TCPSocket.Port.IntValue())
	}

	if container.ReadinessProbe == nil || container.ReadinessProbe.TCPSocket == nil {
		t.Fatalf("expected default readiness probe to be a TCP check, got %#v", container.ReadinessProbe)
	}
	if container.ReadinessProbe.TCPSocket.Port.IntValue() != 8080 {
		t.Errorf("expected default readiness probe to target port 8080, got %d", container.ReadinessProbe.TCPSocket.Port.IntValue())
	}

	if container.StartupProbe != nil {
		t.Errorf("expected no default startup probe, got %#v", container.StartupProbe)
	}
}

func TestDesiredDeployment_DefaultProbesTargetConfiguredPort(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container.Port = 9090

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.LivenessProbe.TCPSocket.Port.IntValue() != 9090 {
		t.Errorf("expected default liveness probe to target configured port 9090, got %d", container.LivenessProbe.TCPSocket.Port.IntValue())
	}
	if container.ReadinessProbe.TCPSocket.Port.IntValue() != 9090 {
		t.Errorf("expected default readiness probe to target configured port 9090, got %d", container.ReadinessProbe.TCPSocket.Port.IntValue())
	}
}

func TestDesiredDeployment_DefaultContainerPort(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.Ports[0].ContainerPort != 8080 {
		t.Fatalf("expected default container port to be 8080, got %d", container.Ports[0].ContainerPort)
	}
}

func TestDesiredDeployment_ConfiguredContainerPort(t *testing.T) {
	port := int32(9090)
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		Port: port,
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	container := deployment.Spec.Template.Spec.Containers[0]

	if container.Ports[0].ContainerPort != 9090 {
		t.Fatalf("expected configured container port to be 9090, got %d", container.Ports[0].ContainerPort)
	}
}

func TestBuildVolumeandMounts_ConfigMap(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		ConfigMapName: testCustomConfigMapName,
	}

	r := &ApplicationReconciler{}
	volumes, volumeMounts := r.buildVolumeAndMounts(app)

	if len(volumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(volumeMounts))
	}

	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(volumes))
	}

	if volumes[0].ConfigMap.Name != testCustomConfigMapName {
		t.Fatalf("expected config map volume name to be %q, got %q", testCustomConfigMapName, volumes[0].ConfigMap.Name)
	}
}

func TestBuildVolumeandMounts_Secret(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		SecretName: testCustomSecretName,
	}

	r := &ApplicationReconciler{}
	volumes, volumeMounts := r.buildVolumeAndMounts(app)

	if len(volumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(volumeMounts))
	}

	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(volumes))
	}

	if volumes[0].Secret.SecretName != testCustomSecretName {
		t.Fatalf("expected secret volume name to be %q, got %q", testCustomSecretName, volumes[0].Secret.SecretName)
	}
}

func TestBuildVolumeandMounts_DefaultConfigMountPath(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		ConfigMapName: testCustomConfigMapName,
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/etc/demo-app/config" {
		t.Fatalf("expected default config mount path to be %q, got %q", "/etc/demo-app/config", volumeMounts[0].MountPath)
	}
}

func TestBuildVolumeandMounts_CustomConfigMountPath(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		ConfigMapName:   testCustomConfigMapName,
		ConfigMountPath: "/custom/config",
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/custom/config" {
		t.Fatalf("expected custom config mount path to be %q, got %q", "/custom/config", volumeMounts[0].MountPath)
	}
}

func TestBuildVolumeandMounts_DefaultSecretMountPath(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		SecretName: testCustomSecretName,
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/etc/demo-app/secret" {
		t.Fatalf("expected default secret mount path to be %q, got %q", "/etc/demo-app/secret", volumeMounts[0].MountPath)
	}
}

func TestBuildVolumeandMounts_CustomSecretMountPath(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		SecretName:      testCustomSecretName,
		SecretMountPath: "/custom/secret",
	}

	r := &ApplicationReconciler{}
	_, volumeMounts := r.buildVolumeAndMounts(app)

	if volumeMounts[0].MountPath != "/custom/secret" {
		t.Fatalf("expected custom secret mount path to be %q, got %q", "/custom/secret", volumeMounts[0].MountPath)
	}
}

func TestConfigMapNameFor(t *testing.T) {
	tests := []struct {
		name          string
		application   string
		configMapName string
		expected      string
	}{
		{
			name:          "uses config map name from spec",
			application:   testAppName,
			configMapName: testCustomConfigMapName,
			expected:      testCustomConfigMapName,
		},
		{
			name:          "uses default config map name when not specified",
			application:   testAppName,
			configMapName: "",
			expected:      testConfigMapName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: tt.application, Namespace: testNamespace},
				Spec: forgev1alpha1.ApplicationSpec{
					Image: testImage,
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
		name        string
		application string
		secretName  string
		expected    string
	}{
		{
			name:        "uses secret name from spec",
			application: testAppName,
			secretName:  testCustomSecretName,
			expected:    testCustomSecretName,
		},
		{
			name:        "uses default secret name when not specified",
			application: testAppName,
			secretName:  "",
			expected:    testSecretAppName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: tt.application, Namespace: testNamespace},
				Spec: forgev1alpha1.ApplicationSpec{
					Image: testImage,
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
		name           string
		serviceAccount *forgev1alpha1.ServiceAccountSpec
		expectedName   string
	}{
		{
			name:           "creates service account when not specified",
			serviceAccount: nil,
			expectedName:   testSAName,
		},
		{
			name: "creates service account when create is true",
			serviceAccount: &forgev1alpha1.ServiceAccountSpec{
				Name: testCustomSAName,
			},
			expectedName: testCustomSAName,
		},
		{
			name: "does not create service account when create is false",
			serviceAccount: &forgev1alpha1.ServiceAccountSpec{
				Create: &doNotCreate,
			},
			expectedName: "",
		},
		{
			name: "creates service account when create is true and name is specified",
			serviceAccount: &forgev1alpha1.ServiceAccountSpec{
				Create: &create,
			},
			expectedName: testSAName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testNamespace},
				Spec: forgev1alpha1.ApplicationSpec{
					Image:          testImage,
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

	app := newTestApplication()

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
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: testDeploymentName, Namespace: testNamespace}, deployment)
	if err != nil {
		t.Fatalf("expected deployment to be created, but got error: %v", err)
	}
}

func TestReconcileDeployment_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	app := newTestApplication()

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
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: testDeploymentName, Namespace: testNamespace}, deployment)
	if err != nil {
		t.Fatalf("expected deployment to exist, but got error: %v", err)
	}

	if deployment.Spec.Template.Spec.Containers[0].Image != testImage {
		t.Fatalf("expected container image to be %q, got %q", testImage, deployment.Spec.Template.Spec.Containers[0].Image)
	}

	if *deployment.Spec.Replicas != 1 {
		t.Fatalf("expected replicas to be 1, got %d", *deployment.Spec.Replicas)
	}
}

func TestReconcileDeployment_UpdatesFields(t *testing.T) {
	tests := []struct {
		name             string
		Image            string
		replicas         int32
		expectedImage    string
		expectedReplicas int32
	}{
		{
			name:             "updates replicas",
			Image:            testImage,
			replicas:         3,
			expectedImage:    testImage,
			expectedReplicas: 3,
		},
		{
			name:             "updates image",
			Image:            testImage127,
			replicas:         1,
			expectedImage:    testImage127,
			expectedReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = forgev1alpha1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			app := newTestApplication()
			app.Spec.Image = tt.Image

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
			err = fakeClient.Get(context.Background(), client.ObjectKey{Name: testDeploymentName, Namespace: testNamespace}, deployment)
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

	app := newTestApplication()
	app.UID = "12345"

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
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: testDeploymentName, Namespace: testNamespace}, deployment)
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
	app := newTestApplication()
	app.Spec.ServiceAccount = &forgev1alpha1.ServiceAccountSpec{
		Name: testCustomSAName,
	}

	r := &ApplicationReconciler{}

	deployment := r.desiredDeployment(app, false)
	serviceAccountName := deployment.Spec.Template.Spec.ServiceAccountName

	if serviceAccountName != testCustomSAName {
		t.Fatalf("expected service account name to be %q, got %q", testCustomSAName, serviceAccountName)
	}
}

func TestReconcileDeployment_UsesResources(t *testing.T) {
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testNamespace},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: testImage,
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
	deployment := r.desiredDeployment(app, false)
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

func TestDesiredDeployment_DefaultsResourcesWhenUnset(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	resources := deployment.Spec.Template.Spec.Containers[0].Resources

	if resources.Requests.Cpu().String() != "100m" {
		t.Errorf("expected default CPU request %q, got %q", "100m", resources.Requests.Cpu().String())
	}
	if resources.Requests.Memory().String() != "128Mi" {
		t.Errorf("expected default memory request %q, got %q", "128Mi", resources.Requests.Memory().String())
	}
	if resources.Limits.Cpu().String() != "500m" {
		t.Errorf("expected default CPU limit %q, got %q", "500m", resources.Limits.Cpu().String())
	}
	if resources.Limits.Memory().String() != "512Mi" {
		t.Errorf("expected default memory limit %q, got %q", "512Mi", resources.Limits.Memory().String())
	}
}

func TestDesiredDeployment_RespectsPartiallySpecifiedResources(t *testing.T) {
	app := newTestApplication()
	app.Spec.Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)
	resources := deployment.Spec.Template.Spec.Containers[0].Resources

	if resources.Limits.Memory().String() != "1Gi" {
		t.Fatalf("expected the user-specified memory limit to be respected, got %q", resources.Limits.Memory().String())
	}
	if len(resources.Requests) != 0 {
		t.Fatalf("expected no requests to be defaulted in when limits were explicitly set, got %#v", resources.Requests)
	}
}

func TestDesiredDeployment_SetsLabels(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	deployment := r.desiredDeployment(app, false)

	expected := testAppName

	if deployment.Spec.Selector.MatchLabels["app"] != expected {
		t.Fatalf("expected selector label 'app' to be %q, got %q", expected, deployment.Spec.Selector.MatchLabels["app"])
	}

	if deployment.Spec.Template.Labels["app"] != expected {
		t.Fatalf("expected template label 'app' to be %q, got %q", expected, deployment.Spec.Template.Labels["app"])
	}
}

func TestBuildVolumeAndMounts_ConfigMapAndSecret(t *testing.T) {
	app := newTestApplication()
	app.Spec.Container = forgev1alpha1.ContainerSpec{
		ConfigMapName: testCustomConfigMapName,
		SecretName:    testCustomSecretName,
	}

	r := &ApplicationReconciler{}
	volumes, volumeMounts := r.buildVolumeAndMounts(app)

	if len(volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(volumes))
	}

	if len(volumeMounts) != 2 {
		t.Fatalf("expected 2 volume mounts, got %d", len(volumeMounts))
	}

	if volumes[0].ConfigMap == nil {
		t.Fatalf("expected first volume to be a ConfigMap volume")
	}
	if volumes[0].ConfigMap.Name != testCustomConfigMapName {
		t.Fatalf("expected ConfigMap volume name to be %q, got %q", testCustomConfigMapName, volumes[0].ConfigMap.Name)
	}

	if volumes[1].Secret == nil {
		t.Fatalf("expected second volume to be a Secret volume")
	}
	if volumes[1].Secret.SecretName != testCustomSecretName {
		t.Fatalf("expected Secret volume name to be %q, got %q", testCustomSecretName, volumes[1].Secret.SecretName)
	}
}

// Unhappy path : Error Handling and Failure Scenarios
func TestReconcileDeployment_ReturnsErrorWhenDeploymentPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	app := newTestApplication()

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

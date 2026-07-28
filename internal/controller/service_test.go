package controller

import (

	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDesiredService(t *testing.T) {
	tests := []struct {
		name        string
		serviceType corev1.ServiceType
		servicePort int32
		containerPort int32
		targetPort  *int32
		expectedType corev1.ServiceType
		expectedPort int32
		expectedTargetPort int32
	}{
		{
			name:        "uses default values",
			serviceType: "",
			servicePort: 0,
			containerPort: 0,
			targetPort:  nil,
			expectedType: corev1.ServiceTypeClusterIP,
			expectedPort: 80,
			expectedTargetPort: 8080,
		},
		{
			name:        "uses custom service type",
			serviceType: corev1.ServiceTypeNodePort,
			servicePort: 8080,
			containerPort: 9090,
			targetPort:  nil,
			expectedType: corev1.ServiceTypeNodePort,
			expectedPort: 8080,
			expectedTargetPort: 9090,
		},
		{
			name:        "uses custom service port",
			serviceType: "",
			servicePort: 3000,
			containerPort: 8080,
			targetPort:  nil,
			expectedType: corev1.ServiceTypeClusterIP,
			expectedPort: 3000,
			expectedTargetPort: 8080,
		},
		{
			name:        "uses container port as target port",
			serviceType: "",
			servicePort: 80,
			containerPort: 9090,
			targetPort:  nil,
			expectedType: corev1.ServiceTypeClusterIP,
			expectedPort: 80,
			expectedTargetPort: 9090,
		},
		{
			name:        "service target port overrides",
			serviceType: "",
			servicePort: 80,
			containerPort: 9090,
			targetPort:  int32Ptr(7000),
			expectedType: corev1.ServiceTypeClusterIP,
			expectedPort: 80,
			expectedTargetPort: 7000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &forgev1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
				Spec: forgev1alpha1.ApplicationSpec{
					Container: forgev1alpha1.ContainerSpec{
						Port: tt.containerPort,
					},
					Service: forgev1alpha1.ServiceSpec{
						Type: tt.serviceType,
						Port: tt.servicePort,
						TargetPort: tt.targetPort,
					},
				},
			}

			r := &ApplicationReconciler{}
			service := r.desiredService(app)

			if service.Spec.Type != tt.expectedType {
				t.Errorf("expected service type %q, got %q", tt.expectedType, service.Spec.Type)
			}

			if service.Spec.Ports[0].Port != tt.expectedPort {
				t.Errorf("expected service port %d, got %d", tt.expectedPort, service.Spec.Ports[0].Port)
			}

			gotTargetPort := service.Spec.Ports[0].TargetPort.IntVal
			if gotTargetPort != tt.expectedTargetPort {
				t.Errorf("expected service target port %d, got %d", tt.expectedTargetPort, gotTargetPort)
			}
		})
	}
}



func int32Ptr(i int32) *int32 {
	return &i
}
			
func TestReconcileService_CreateService(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Service: forgev1alpha1.ServiceSpec{
				Port: 80,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ApplicationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := r.reconcileService(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileService returned error: %v", err)
	}

	service := &corev1.Service{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, service)
	if err != nil {
		t.Fatalf("failed to get Service: %v", err)
	}

	if service.Spec.Ports[0].Port != 80 {
		t.Errorf("expected service port 80, got %d", service.Spec.Ports[0].Port)
	}
}

func TestReconcileService_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Service: forgev1alpha1.ServiceSpec{
				Port: 80,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ApplicationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// First reconciliation
	err := r.reconcileService(context.Background(), app)
	if err != nil {
		t.Fatalf("first reconcileService returned error: %v", err)
	}

	// Second reconciliation
	err = r.reconcileService(context.Background(), app)
	if err != nil {
		t.Fatalf("second reconcileService returned error: %v", err)
	}

	service := &corev1.Service{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, service)
	if err != nil {
		t.Fatalf("failed to get Service after second reconciliation: %v", err)
	}

	if service.Spec.Ports[0].Port != 80 {
		t.Errorf("expected service port 80 after second reconciliation, got %d", service.Spec.Ports[0].Port)
	}
}

func TestReconcileService_UpdateServicePort(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Service: forgev1alpha1.ServiceSpec{
				Port: 80,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ApplicationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// First reconciliation creates service with port 80
	err := r.reconcileService(context.Background(), app)
	if err != nil {
		t.Fatalf("first reconcileService returned error: %v", err)
	}

	// change desired state
	app.Spec.Service.Port = 8080

	// Second reconciliation should update the service port to 8080
	err = r.reconcileService(context.Background(), app)
	if err != nil {
		t.Fatalf("second reconcileService returned error: %v", err)
	}

	service := &corev1.Service{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, service)
	if err != nil {
		t.Fatalf("failed to get Service after second reconciliation: %v", err)
	}

	if service.Spec.Ports[0].Port != 8080 {
		t.Errorf("expected service port 8080 after second reconciliation, got %d", service.Spec.Ports[0].Port)
	}
}

func TestReconcileService_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default", UID: "12345"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Service: forgev1alpha1.ServiceSpec{
				Port: 80,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ApplicationReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	err := r.reconcileService(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileService returned error: %v", err)
	}

	service := &corev1.Service{}
	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, service)
	if err != nil {
		t.Fatalf("failed to get Service: %v", err)
	}

	if len(service.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(service.OwnerReferences))
	}

	owner := service.OwnerReferences[0]
	if owner.Name != app.Name {
		t.Errorf("expected owner reference name %q, got %q", app.Name, owner.Name)
	}
	if owner.Kind != "Application" {
		t.Errorf("expected owner reference kind 'Application', got %q", owner.Kind)
	}
}

// Unhappy path : Error Handling and Failure Scenarios

func TestReconcileService_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()

	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default", UID: "12345"},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx:latest",
			Service: forgev1alpha1.ServiceSpec{
				Port: 80,
			},
		},
	}

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ApplicationReconciler{
		Client: &failingPatchClient{
			Client: baseClient,
		},
		Scheme: scheme,
	}

	err := r.reconcileService(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error from reconcileService, got nil")
	}
}
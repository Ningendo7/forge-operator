package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDesiredHPA_ReturnsNilWhenAutoscalingDisabled(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	hpa := r.desiredHPA(app)

	if hpa != nil {
		t.Fatalf("expected nil HPA when autoscaling is not configured, got %#v", hpa)
	}
}

func TestDesiredHPA_DefaultsMinMaxReplicasAndUtilization(t *testing.T) {
	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{}

	r := &ApplicationReconciler{}
	hpa := r.desiredHPA(app)

	if hpa.Name != testHPAName {
		t.Fatalf("expected hpa name %q, got %q", testHPAName, hpa.Name)
	}
	if *hpa.Spec.MinReplicas != 1 {
		t.Fatalf("expected default minReplicas 1, got %d", *hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 3 {
		t.Fatalf("expected default maxReplicas 3, got %d", hpa.Spec.MaxReplicas)
	}
	if *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != 80 {
		t.Fatalf("expected default cpu utilization 80, got %d", *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	}
}

func TestDesiredHPA_UsesConfiguredMinMaxReplicas(t *testing.T) {
	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{
		MinReplicas: 2,
		MaxReplicas: 5,
	}

	r := &ApplicationReconciler{}
	hpa := r.desiredHPA(app)

	if *hpa.Spec.MinReplicas != 2 {
		t.Fatalf("expected minReplicas 2, got %d", *hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 5 {
		t.Fatalf("expected maxReplicas 5, got %d", hpa.Spec.MaxReplicas)
	}
}

func TestDesiredHPA_ClampsMaxReplicasToMinReplicas(t *testing.T) {
	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{
		MinReplicas: 5,
		MaxReplicas: 2,
	}

	r := &ApplicationReconciler{}
	hpa := r.desiredHPA(app)

	if hpa.Spec.MaxReplicas != 5 {
		t.Fatalf("expected maxReplicas to be clamped up to minReplicas 5, got %d", hpa.Spec.MaxReplicas)
	}
}

func TestDesiredHPA_ClampsCPUUtilizationToValidRange(t *testing.T) {
	tests := []struct {
		name     string
		input    int32
		expected int32
	}{
		{name: "clamps below 1 up to 1", input: -10, expected: 1},
		{name: "clamps above 100 down to 100", input: 150, expected: 100},
		{name: "keeps value within range", input: 50, expected: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApplication()
			app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{
				CPUUtilization: &tt.input,
			}

			r := &ApplicationReconciler{}
			hpa := r.desiredHPA(app)

			if *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != tt.expected {
				t.Fatalf("expected cpu utilization %d, got %d", tt.expected, *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
			}
		})
	}
}

func TestDesiredHPA_ScaleTargetRefTargetsDeployment(t *testing.T) {
	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{}

	r := &ApplicationReconciler{}
	hpa := r.desiredHPA(app)

	if hpa.Spec.ScaleTargetRef.Kind != deploymentKind {
		t.Fatalf("expected scale target kind Deployment, got %q", hpa.Spec.ScaleTargetRef.Kind)
	}
	if hpa.Spec.ScaleTargetRef.Name != testDeploymentName {
		t.Fatalf("expected scale target name demo-app-deployment, got %q", hpa.Spec.ScaleTargetRef.Name)
	}
}

func TestReconcileHPA_CreatesHPA(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileHPA(context.Background(), app); err != nil {
		t.Fatalf("reconcileHPA returned error: %v", err)
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testHPAName, Namespace: testNamespace}, hpa); err != nil {
		t.Fatalf("failed to get HPA: %v", err)
	}
	if hpa.Spec.MaxReplicas != 3 {
		t.Errorf("expected maxReplicas 3, got %d", hpa.Spec.MaxReplicas)
	}
}

func TestReconcileHPA_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileHPA(context.Background(), app); err != nil {
		t.Fatalf("first reconcileHPA returned error: %v", err)
	}
	if err := r.reconcileHPA(context.Background(), app); err != nil {
		t.Fatalf("second reconcileHPA returned error: %v", err)
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testHPAName, Namespace: testNamespace}, hpa); err != nil {
		t.Fatalf("failed to get HPA after second reconciliation: %v", err)
	}
}

func TestReconcileHPA_DeletesWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileHPA(context.Background(), app); err != nil {
		t.Fatalf("failed to create HPA: %v", err)
	}

	app.Spec.Autoscaling = nil
	if err := r.reconcileHPA(context.Background(), app); err != nil {
		t.Fatalf("reconcileHPA returned error on disable: %v", err)
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testHPAName, Namespace: testNamespace}, hpa)
	if err == nil {
		t.Fatalf("expected HPA to be deleted, but it still exists")
	}
}

func TestReconcileHPA_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)

	app := newTestApplication()
	app.UID = "12345"
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileHPA(context.Background(), app); err != nil {
		t.Fatalf("reconcileHPA returned error: %v", err)
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testHPAName, Namespace: testNamespace}, hpa); err != nil {
		t.Fatalf("failed to get HPA: %v", err)
	}

	if len(hpa.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(hpa.OwnerReferences))
	}
	if hpa.OwnerReferences[0].Name != app.Name {
		t.Errorf("expected owner reference name %q, got %q", app.Name, hpa.OwnerReferences[0].Name)
	}
}

// Unhappy path : Error Handling and Failure Scenarios

func TestReconcileHPA_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Autoscaling = &forgev1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 3}

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: &failingPatchClient{Client: baseClient},
		Scheme: scheme,
	}

	err := r.reconcileHPA(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error from reconcileHPA, got nil")
	}
}

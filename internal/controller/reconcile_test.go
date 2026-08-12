package controller

import (
	"context"
	"testing"
	"time"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	statusmanager "github.com/Ningendo7/forge-operator/internal/controller/status"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newReconcileScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)
	_ = autoscalingv2.AddToScheme(scheme)
	return scheme
}

func TestReconcile_ReturnsNilWhenApplicationNotFound(t *testing.T) {
	scheme := newReconcileScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		StatusManager: statusmanager.NewStatusManager(fakeClient),
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Name: "missing-app", Namespace: testNamespace},
	})
	if err != nil {
		t.Fatalf("expected nil error when Application is not found, got %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue, got %v", result.RequeueAfter)
	}
}

func TestReconcile_SetsFailedStatusWhenEnsureDesiredStateFails(t *testing.T) {
	scheme := newReconcileScheme()
	app := newTestApplication()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{
		Client:        &failingPatchClient{Client: fakeClient},
		Scheme:        scheme,
		StatusManager: statusmanager.NewStatusManager(fakeClient),
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Name: testAppName, Namespace: testNamespace},
	})
	if err == nil {
		t.Fatalf("expected error when ensureDesiredState fails, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}

	degraded := findAppCondition(got, statusmanager.TypeDegraded)
	if degraded == nil || degraded.Status != "True" {
		t.Fatalf("expected Degraded=True after ensureDesiredState failure, got %#v", degraded)
	}
}

func TestReconcile_RequeuesWhenComputeNotYetReady(t *testing.T) {
	scheme := newReconcileScheme()
	app := newTestApplication()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		StatusManager: statusmanager.NewStatusManager(fakeClient),
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Name: testAppName, Namespace: testNamespace},
	})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 10*time.Second {
		t.Fatalf("expected RequeueAfter=10s while not ready, got %v", result.RequeueAfter)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	ready := findAppCondition(got, statusmanager.TypeReady)
	if ready == nil || ready.Status != testConditionFalse {
		t.Fatalf("expected Ready=False while compute is not yet healthy, got %#v", ready)
	}

	dep := &appsv1.Deployment{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testDeploymentName, Namespace: testNamespace}, dep); err != nil {
		t.Fatalf("expected Deployment to have been created by ensureDesiredState: %v", err)
	}
}

func TestReconcile_SetsReadyWhenComputeIsHealthy(t *testing.T) {
	scheme := newReconcileScheme()
	app := newTestApplication()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		StatusManager: statusmanager.NewStatusManager(fakeClient),
	}

	// First reconcile creates the child resources (Deployment will not be ready yet).
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Name: testAppName, Namespace: testNamespace},
	}); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}

	// Simulate the Deployment becoming healthy, as a real deployment controller would report.
	dep := &appsv1.Deployment{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testDeploymentName, Namespace: testNamespace}, dep); err != nil {
		t.Fatalf("failed to get Deployment: %v", err)
	}
	dep.Status.UpdatedReplicas = 1
	dep.Status.AvailableReplicas = 1
	dep.Status.ReadyReplicas = 1
	if err := fakeClient.Status().Update(context.Background(), dep); err != nil {
		t.Fatalf("failed to update Deployment status: %v", err)
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Name: testAppName, Namespace: testNamespace},
	})
	if err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue once compute is healthy, got %v", result.RequeueAfter)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	ready := findAppCondition(got, statusmanager.TypeReady)
	if ready == nil || ready.Status != "True" {
		t.Fatalf("expected Ready=True once compute is healthy, got %#v", ready)
	}
}

func TestReconcile_ReturnsEarlyWhenDeleting(t *testing.T) {
	scheme := newReconcileScheme()
	app := newTestApplication()
	app.Finalizers = []string{ApplicationFinalizer}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		StatusManager: statusmanager.NewStatusManager(fakeClient),
	}

	if err := fakeClient.Delete(context.Background(), app); err != nil {
		t.Fatalf("failed to delete application: %v", err)
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Name: testAppName, Namespace: testNamespace},
	})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue on deletion path, got %v", result.RequeueAfter)
	}

	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, &forgev1alpha1.Application{})
	if err == nil {
		t.Fatalf("expected Application to be fully removed after finalizer cleanup")
	}
}

func findAppCondition(app *forgev1alpha1.Application, condType string) *metav1.Condition {
	for i := range app.Status.Conditions {
		if app.Status.Conditions[i].Type == condType {
			return &app.Status.Conditions[i]
		}
	}
	return nil
}

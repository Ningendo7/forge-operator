package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDesiredConfigMap_DefaultsToAppNameAndImage(t *testing.T) {
	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{
		Data: map[string]string{"foo": "bar"},
	}

	r := &ApplicationReconciler{}
	cm := r.desiredConfigMap(app)

	if cm.Name != "demo-app-config" {
		t.Fatalf("expected default config map name demo-app-config, got %q", cm.Name)
	}
	if cm.Data["foo"] != "bar" {
		t.Fatalf("expected configured data foo=bar to be present, got %v", cm.Data)
	}
}

func TestDesiredConfigMap_UsesConfiguredValues(t *testing.T) {
	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{
		Name: "custom-config",
		Data: map[string]string{"foo": "bar"},
	}

	r := &ApplicationReconciler{}
	cm := r.desiredConfigMap(app)

	if cm.Name != "custom-config" {
		t.Fatalf("expected configured config map name custom-config, got %q", cm.Name)
	}
	if cm.Data["foo"] != "bar" {
		t.Fatalf("expected configured data foo=bar to be present, got %v", cm.Data)
	}
}

func TestDesiredConfigMap_FallsBackToDefaultDataWhenUnset(t *testing.T) {
	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{Name: "custom-config"}

	r := &ApplicationReconciler{}
	cm := r.desiredConfigMap(app)

	if cm.Data["app-name"] != app.Name {
		t.Fatalf("expected default app-name data to be %q, got %q", app.Name, cm.Data["app-name"])
	}
	if cm.Data["image"] != app.Spec.Image {
		t.Fatalf("expected default image data to be %q, got %q", app.Spec.Image, cm.Data["image"])
	}
}

func TestDesiredConfigMap_SetsLabels(t *testing.T) {
	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{Data: map[string]string{"foo": "bar"}}

	r := &ApplicationReconciler{}
	cm := r.desiredConfigMap(app)

	if cm.Labels["app"] != app.Name {
		t.Fatalf("expected label 'app' to be %q, got %q", app.Name, cm.Labels["app"])
	}
}

func TestReconcileConfigMap_CreatesConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{
		Data: map[string]string{"foo": "bar"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("reconcileConfigMap returned error: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-config", Namespace: "default"}, cm); err != nil {
		t.Fatalf("failed to get ConfigMap: %v", err)
	}
	if cm.Data["foo"] != "bar" {
		t.Errorf("expected configmap data foo=bar to be present")
	}
}

func TestReconcileConfigMap_CreatesWhenSpecPresentButDataEmpty(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("reconcileConfigMap returned error: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-config", Namespace: "default"}, cm); err != nil {
		t.Fatalf("expected ConfigMap to be created even with empty data, got: %v", err)
	}
	if cm.Data["app-name"] != app.Name {
		t.Errorf("expected default data to be used, got %v", cm.Data)
	}
}

func TestReconcileConfigMap_RenameCleansUpPreviousName(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{Name: "old-name", Data: map[string]string{"foo": "bar"}}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("failed to create configmap: %v", err)
	}

	app.Spec.ConfigMap.Name = "new-name"
	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("reconcileConfigMap returned error on rename: %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "new-name", Namespace: "default"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("expected new-name ConfigMap to exist: %v", err)
	}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "old-name", Namespace: "default"}, &corev1.ConfigMap{})
	if err == nil {
		t.Fatalf("expected old-name ConfigMap to have been cleaned up after rename")
	}
}

func TestReconcileConfigMap_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{
		Data: map[string]string{"foo": "bar"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("first reconcileConfigMap returned error: %v", err)
	}
	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("second reconcileConfigMap returned error: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-config", Namespace: "default"}, cm); err != nil {
		t.Fatalf("failed to get ConfigMap after second reconciliation: %v", err)
	}
}

func TestReconcileConfigMap_DeletesWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{
		Data: map[string]string{"foo": "bar"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("failed to create configmap: %v", err)
	}

	app.Spec.ConfigMap = nil
	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("reconcileConfigMap returned error on disable: %v", err)
	}

	cm := &corev1.ConfigMap{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-config", Namespace: "default"}, cm)
	if err == nil {
		t.Fatalf("expected ConfigMap to be deleted, but it still exists")
	}
}

func TestReconcileConfigMap_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.ObjectMeta.UID = "12345"
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{
		Data: map[string]string{"foo": "bar"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileConfigMap(context.Background(), app); err != nil {
		t.Fatalf("reconcileConfigMap returned error: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-config", Namespace: "default"}, cm); err != nil {
		t.Fatalf("failed to get ConfigMap: %v", err)
	}

	if len(cm.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(cm.OwnerReferences))
	}
	if cm.OwnerReferences[0].Name != app.Name {
		t.Errorf("expected owner reference name %q, got %q", app.Name, cm.OwnerReferences[0].Name)
	}
}

// Unhappy path : Error Handling and Failure Scenarios

func TestReconcileConfigMap_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.ConfigMap = &forgev1alpha1.ConfigSpec{
		Data: map[string]string{"foo": "bar"},
	}

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: &failingPatchClient{Client: baseClient},
		Scheme: scheme,
	}

	err := r.reconcileConfigMap(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error from reconcileConfigMap, got nil")
	}
}

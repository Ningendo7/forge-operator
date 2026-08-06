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

func TestDesiredSecret_DefaultsToOpaqueType(t *testing.T) {
	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	r := &ApplicationReconciler{}
	secret := r.desiredSecret(app)

	if secret.Name != "demo-app-secret" {
		t.Fatalf("expected default secret name demo-app-secret, got %q", secret.Name)
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Fatalf("expected default secret type %q, got %q", corev1.SecretTypeOpaque, secret.Type)
	}
	if secret.StringData["API_KEY"] != "abc123" {
		t.Fatalf("expected secret data API_KEY to be present")
	}
}

func TestDesiredSecret_UsesConfiguredValues(t *testing.T) {
	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		Name:       "custom-secret",
		StringData: map[string]string{"API_KEY": "abc123"},
		Type:       corev1.SecretTypeOpaque,
	}

	r := &ApplicationReconciler{}
	secret := r.desiredSecret(app)

	if secret.Name != "custom-secret" {
		t.Fatalf("expected secret name custom-secret, got %q", secret.Name)
	}
	if secret.StringData["API_KEY"] != "abc123" {
		t.Fatalf("expected secret data API_KEY to be present")
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Fatalf("expected secret type %q, got %q", corev1.SecretTypeOpaque, secret.Type)
	}
}

func TestDesiredSecret_SetsLabels(t *testing.T) {
	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{StringData: map[string]string{"a": "b"}}

	r := &ApplicationReconciler{}
	secret := r.desiredSecret(app)

	if secret.Labels["app"] != app.Name {
		t.Fatalf("expected label 'app' to be %q, got %q", app.Name, secret.Labels["app"])
	}
}

func TestReconcileSecret_CreatesSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-secret", Namespace: "default"}, secret); err != nil {
		t.Fatalf("failed to get Secret: %v", err)
	}
	if secret.StringData["API_KEY"] != "abc123" {
		t.Errorf("expected secret data API_KEY to be present")
	}
}

func TestReconcileSecret_CreatesWhenSpecPresentButDataEmpty(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-secret", Namespace: "default"}, secret); err != nil {
		t.Fatalf("expected Secret to be created even with empty data, got: %v", err)
	}
}

func TestReconcileSecret_UsesManagedNameIndependentOfContainerMountRef(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Container.SecretName = "some-other-preexisting-secret"
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		Name:       "custom-managed-secret",
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	// Disabling spec.secret must delete the managed secret, not the unrelated
	// Container.SecretName mount reference.
	app.Spec.Secret = nil
	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileSecret returned error on disable: %v", err)
	}

	deleted := &corev1.Secret{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "custom-managed-secret", Namespace: "default"}, deleted)
	if err == nil {
		t.Fatalf("expected managed Secret custom-managed-secret to be deleted, but it still exists")
	}
}

func TestReconcileSecret_RenameCleansUpPreviousName(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		Name:       "old-name",
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	app.Spec.Secret.Name = "new-name"
	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileSecret returned error on rename: %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "new-name", Namespace: "default"}, &corev1.Secret{}); err != nil {
		t.Fatalf("expected new-name Secret to exist: %v", err)
	}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "old-name", Namespace: "default"}, &corev1.Secret{})
	if err == nil {
		t.Fatalf("expected old-name Secret to have been cleaned up after rename")
	}
}

func TestReconcileSecret_DisablingAppSecretDoesNotDeleteStorageSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{StringData: map[string]string{"API_KEY": "abc123"}}
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: "demo-bucket"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("failed to create app secret: %v", err)
	}
	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
		t.Fatalf("failed to create storage secret: %v", err)
	}

	app.Spec.Secret = nil
	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileSecret returned error on disable: %v", err)
	}

	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-storage", Namespace: "default"}, &corev1.Secret{}); err != nil {
		t.Fatalf("expected storage Secret to survive disabling the unrelated app Secret: %v", err)
	}
}

func TestReconcileSecret_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("first reconcileSecret returned error: %v", err)
	}
	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("second reconcileSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-secret", Namespace: "default"}, secret); err != nil {
		t.Fatalf("failed to get Secret after second reconciliation: %v", err)
	}
}

func TestReconcileSecret_DeletesWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	app.Spec.Secret = nil
	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileSecret returned error on disable: %v", err)
	}

	secret := &corev1.Secret{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-secret", Namespace: "default"}, secret)
	if err == nil {
		t.Fatalf("expected Secret to be deleted, but it still exists")
	}
}

func TestReconcileSecret_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.ObjectMeta.UID = "12345"
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-secret", Namespace: "default"}, secret); err != nil {
		t.Fatalf("failed to get Secret: %v", err)
	}

	if len(secret.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(secret.OwnerReferences))
	}
	if secret.OwnerReferences[0].Name != app.Name {
		t.Errorf("expected owner reference name %q, got %q", app.Name, secret.OwnerReferences[0].Name)
	}
}

// Unhappy path : Error Handling and Failure Scenarios

func TestReconcileSecret_ReturnsErrorWhenPatchFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Secret = &forgev1alpha1.SecretSpec{
		StringData: map[string]string{"API_KEY": "abc123"},
	}

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{
		Client: &failingPatchClient{Client: baseClient},
		Scheme: scheme,
	}

	err := r.reconcileSecret(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error from reconcileSecret, got nil")
	}
}

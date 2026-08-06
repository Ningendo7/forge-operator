package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// --- handleFinalizer ---

func TestHandleFinalizer_AddsFinalizerWhenNotDeleting(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	deleted, err := r.handleFinalizer(context.Background(), app)
	if err != nil {
		t.Fatalf("handleFinalizer returned error: %v", err)
	}
	if deleted {
		t.Fatalf("expected deleted=false for an active application")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	found := false
	for _, f := range got.Finalizers {
		if f == ApplicationFinalizer {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected finalizer %q to be added, got %v", ApplicationFinalizer, got.Finalizers)
	}
}

func TestHandleFinalizer_NoOpWhenFinalizerAlreadyPresent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Finalizers = []string{ApplicationFinalizer}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	deleted, err := r.handleFinalizer(context.Background(), app)
	if err != nil {
		t.Fatalf("handleFinalizer returned error: %v", err)
	}
	if deleted {
		t.Fatalf("expected deleted=false for an active application")
	}
}

func TestHandleFinalizer_ReturnsTrueWithoutCleanupWhenFinalizerAbsentOnDelete(t *testing.T) {
	app := newTestApplication()
	now := metav1.Now()
	app.DeletionTimestamp = &now

	r := &ApplicationReconciler{}

	deleted, err := r.handleFinalizer(context.Background(), app)
	if err != nil {
		t.Fatalf("handleFinalizer returned error: %v", err)
	}
	if !deleted {
		t.Fatalf("expected deleted=true when object has a deletion timestamp")
	}
}

func TestHandleFinalizer_RemovesFinalizerOnDeleteWithNoStorage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Finalizers = []string{ApplicationFinalizer}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := fakeClient.Delete(context.Background(), app); err != nil {
		t.Fatalf("failed to delete application: %v", err)
	}

	pending := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, pending); err != nil {
		t.Fatalf("failed to get pending-deletion Application: %v", err)
	}

	deleted, err := r.handleFinalizer(context.Background(), pending)
	if err != nil {
		t.Fatalf("handleFinalizer returned error: %v", err)
	}
	if !deleted {
		t.Fatalf("expected deleted=true")
	}

	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, &forgev1alpha1.Application{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected Application to be fully removed after finalizer cleanup, got err=%v", err)
	}
}

func TestHandleFinalizer_ReturnsErrorAndKeepsFinalizerWhenCleanupFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Finalizers = []string{ApplicationFinalizer}
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   "demo-bucket",
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := fakeClient.Delete(context.Background(), app); err != nil {
		t.Fatalf("failed to delete application: %v", err)
	}

	pending := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, pending); err != nil {
		t.Fatalf("failed to get pending-deletion Application: %v", err)
	}

	deleted, err := r.handleFinalizer(context.Background(), pending)
	if err == nil {
		t.Fatalf("expected error when storage cleanup fails, got nil")
	}
	if !deleted {
		t.Fatalf("expected deleted=true even on cleanup failure")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, got); err != nil {
		t.Fatalf("expected Application to still exist after failed cleanup: %v", err)
	}
	found := false
	for _, f := range got.Finalizers {
		if f == ApplicationFinalizer {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected finalizer to remain after failed cleanup, got %v", got.Finalizers)
	}
}

// --- finalizeApplication ---

func TestFinalizeApplication_NoOpWhenStorageIsNil(t *testing.T) {
	app := newTestApplication()
	r := &ApplicationReconciler{}

	if err := r.finalizeApplication(context.Background(), app); err != nil {
		t.Fatalf("expected nil error when storage spec is nil, got %v", err)
	}
}

func TestFinalizeApplication_NoOpForUnrecognizedProvider(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider: "Static",
		Bucket:   "demo-bucket",
	}
	r := &ApplicationReconciler{}

	if err := r.finalizeApplication(context.Background(), app); err != nil {
		t.Fatalf("expected nil error for unrecognized provider, got %v", err)
	}
}

func TestFinalizeApplication_ReturnsErrorWhenAWSManagerCreationFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     "demo-bucket",
		SecretName: "missing-creds",
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.finalizeApplication(context.Background(), app); err == nil {
		t.Fatalf("expected error when AWS storage manager creation fails, got nil")
	}
}

func TestFinalizeApplication_ReturnsErrorWhenAkamaiManagerCreationFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:   "demo-bucket",
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.finalizeApplication(context.Background(), app); err == nil {
		t.Fatalf("expected error when Akamai storage manager creation fails, got nil")
	}
}

func TestFinalizeApplication_SetsStorageReadyCleanupFailedOnError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     "demo-bucket",
		SecretName: "missing-creds",
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.finalizeApplication(context.Background(), app); err == nil {
		t.Fatalf("expected error when AWS storage manager creation fails, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}

	var storageReady *metav1.Condition
	for i := range got.Status.Conditions {
		if got.Status.Conditions[i].Type == "StorageReady" {
			storageReady = &got.Status.Conditions[i]
		}
	}
	if storageReady == nil {
		t.Fatalf("expected StorageReady condition to be set after cleanup failure")
	}
	if storageReady.Reason != "BucketCleanupFailed" {
		t.Fatalf("expected reason BucketCleanupFailed, got %q", storageReady.Reason)
	}
}

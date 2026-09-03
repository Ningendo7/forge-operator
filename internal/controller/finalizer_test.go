package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/storagestatus"
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
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
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
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, pending); err != nil {
		t.Fatalf("failed to get pending-deletion Application: %v", err)
	}

	deleted, err := r.handleFinalizer(context.Background(), pending)
	if err != nil {
		t.Fatalf("handleFinalizer returned error: %v", err)
	}
	if !deleted {
		t.Fatalf("expected deleted=true")
	}

	err = fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, &forgev1alpha1.Application{})
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
		Bucket:   testBucket,
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := fakeClient.Delete(context.Background(), app); err != nil {
		t.Fatalf("failed to delete application: %v", err)
	}

	pending := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, pending); err != nil {
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
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
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
		Provider: "SomeFutureProvider",
		Bucket:   testBucket,
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
		Bucket:     testBucket,
		SecretName: testMissingCredsSecret,
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
		Bucket:   testBucket,
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
		Bucket:     testBucket,
		SecretName: testMissingCredsSecret,
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.finalizeApplication(context.Background(), app); err == nil {
		t.Fatalf("expected error when AWS storage manager creation fails, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
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

// --- retainStorage / deletionPolicy: Retain ---

func TestFinalizeApplication_RetainSkipsCloudCleanupForAWS(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:       forgev1alpha1.ProviderAWSS3,
		Bucket:         testBucket,
		DeletionPolicy: forgev1alpha1.DeletionPolicyRetain,
		// A real AWS manager would fail to construct (no credentials Secret
		// configured) -- if retention actually skips the provider switch as
		// intended, that failure is never reached, so this deliberately
		// invalid config alone proves the cloud path wasn't taken.
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	rec := &fakeEventRecorder{}
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme, Recorder: rec}

	if err := r.finalizeApplication(context.Background(), app); err != nil {
		t.Fatalf("expected nil error when retaining storage, got %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	var storageReady *metav1.Condition
	for i := range got.Status.Conditions {
		if got.Status.Conditions[i].Type == "StorageReady" {
			storageReady = &got.Status.Conditions[i]
		}
	}
	if storageReady == nil {
		t.Fatalf("expected StorageReady condition to be set")
	}
	if storageReady.Reason != storagestatus.ReasonBucketRetained {
		t.Fatalf("expected reason %q, got %q", storagestatus.ReasonBucketRetained, storageReady.Reason)
	}

	if len(rec.events) != 1 {
		t.Fatalf("expected exactly one Event to be recorded, got %d", len(rec.events))
	}
	if rec.events[0].reason != "StorageRetained" {
		t.Fatalf("expected reason StorageRetained, got %q", rec.events[0].reason)
	}
	if rec.events[0].eventtype != "Normal" {
		t.Fatalf("expected a Normal event (this is intentional, not a failure), got %q", rec.events[0].eventtype)
	}
}

func TestFinalizeApplication_RetainSkipsCloudCleanupForAkamai(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:       forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:         testBucket,
		DeletionPolicy: forgev1alpha1.DeletionPolicyRetain,
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	// An Akamai manager would also fail to construct here (no token Secret)
	// -- same reasoning as the AWS case above.
	if err := r.finalizeApplication(context.Background(), app); err != nil {
		t.Fatalf("expected nil error when retaining storage, got %v", err)
	}
}

func TestFinalizeApplication_DeleteIsStillDefaultBehavior(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     testBucket,
		SecretName: testMissingCredsSecret,
		// DeletionPolicy deliberately left unset.
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	// An unset DeletionPolicy must still take the normal cleanup path (and
	// therefore hit the same real-manager-construction error as the
	// existing AWS test above) -- retention must never be the accidental
	// default.
	err := r.finalizeApplication(context.Background(), app)
	if err == nil {
		t.Fatalf("expected the normal cleanup path (and its error) when deletionPolicy is unset, got nil error")
	}
}

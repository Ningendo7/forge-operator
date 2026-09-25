package controller

import (
	"context"
	"strings"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	"github.com/Ningendo7/forge-operator/internal/controller/storagestatus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
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

func TestFinalizeApplication_UsesStatusStorageWhenSpecStorageIsNil(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)

	// Simulates an Application deleted after spec.storage was already
	// removed, before the cleanup that removal should have triggered ever
	// ran (see reconcileStorage) -- Status.Storage is the only remaining
	// record of what to clean up. The missing credentials Secret proves the
	// AWS cleanup path was actually entered (manager construction fails)
	// rather than silently no-op'd the way TestFinalizeApplication_NoOpWhenStorageIsNil
	// correctly does when there's no prior status either.
	app := newTestApplication()
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     testBucket,
		SecretName: testMissingCredsSecret,
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.finalizeApplication(context.Background(), app); err == nil {
		t.Fatalf("expected error: cleanup should have been attempted using Status.Storage, got nil")
	}
}

// --- storageSpecFromStatus ---

func TestStorageSpecFromStatus_OmitsAkamaiWhenNotRecorded(t *testing.T) {
	status := &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:   testBucket,
	}

	spec := storageSpecFromStatus(status)

	if spec.Akamai != nil {
		t.Fatalf("expected a nil Akamai block when status never recorded one, got %#v", spec.Akamai)
	}
}

func TestStorageSpecFromStatus_PreservesCustomAkamaiAccessKeySecretRef(t *testing.T) {
	// The actual bug: akamaiobjstr.NewManager resolves the input token
	// Secret via naming.AkamaiTokenSecret(app), which falls back to a
	// default name whenever Spec.Storage.Akamai is nil -- exactly what a
	// naively-reconstructed StorageSpec (missing this field) would produce.
	// A customized accessKeySecretRef must survive the Status round-trip
	// intact, not fall back to the default.
	const customTokenSecret = "my-custom-akamai-token"
	status := &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:   testBucket,
		Akamai:   &forgev1alpha1.AkamaiStorageStatus{AccessKeySecretRef: customTokenSecret},
	}

	spec := storageSpecFromStatus(status)

	if spec.Akamai == nil || spec.Akamai.AccessKeySecretRef != customTokenSecret {
		t.Fatalf("expected AccessKeySecretRef %q to be carried forward, got %#v", customTokenSecret, spec.Akamai)
	}
}

func TestStorageSpecFromStatus_OmitsAWSWhenNotRecorded(t *testing.T) {
	status := &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   testBucket,
	}

	spec := storageSpecFromStatus(status)

	if spec.AWS != nil {
		t.Fatalf("expected a nil AWS block when status never recorded a credentialsSecretRef, got %#v", spec.AWS)
	}
}

func TestStorageSpecFromStatus_PreservesCustomAWSCredentialsSecretRef(t *testing.T) {
	// s3storage.NewManager resolves static credentials via
	// Spec.Storage.AWS.CredentialsSecretRef, which is empty whenever
	// Spec.Storage.AWS is nil -- exactly what a naively-reconstructed
	// StorageSpec (missing this field) would produce. A customized
	// credentialsSecretRef must survive the Status round-trip intact.
	const customCredsSecret = "my-custom-aws-creds"
	status := &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   testBucket,
		AWS:      &forgev1alpha1.AWSStorageStatus{CredentialsSecretRef: customCredsSecret},
	}

	spec := storageSpecFromStatus(status)

	if spec.AWS == nil || spec.AWS.CredentialsSecretRef != customCredsSecret {
		t.Fatalf("expected CredentialsSecretRef %q to be carried forward, got %#v", customCredsSecret, spec.AWS)
	}
}

func TestFinalizeApplication_UsesCustomAkamaiAccessKeySecretRefFromStatus(t *testing.T) {
	// End-to-end version of the same bug, at the level a real deletion
	// actually exercises: spec.storage already removed, Status.Storage is
	// the only remaining record, and the token Secret only exists under the
	// application's own *customized* name -- never the default
	// "<app>-akamai-token" naming.AkamaiTokenSecret would fall back to.
	// Before this fix, cleanup looked for the wrong (default) name here and
	// failed to find a Secret that genuinely existed.
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	const customTokenSecret = "my-custom-akamai-token"
	app := newTestApplication()
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:   testBucket,
		Akamai:   &forgev1alpha1.AkamaiStorageStatus{AccessKeySecretRef: customTokenSecret},
	}

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: customTokenSecret, Namespace: testNamespace},
		Data:       map[string][]byte{"apiToken": []byte("token-value")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tokenSecret).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	// akamaiobjstr.NewManager makes a real API call once it successfully
	// finds the token Secret, so this is expected to fail past that point --
	// the assertion is specifically that it does NOT fail trying to find
	// "<app>-akamai-token" (the default name), which is what the pre-fix
	// behavior did.
	err := r.finalizeApplication(context.Background(), app)
	if err == nil {
		t.Fatalf("expected an error (no real Akamai account reachable in this test), got nil")
	}
	if defaultName := app.Name + "-akamai-token"; strings.Contains(err.Error(), defaultName) {
		t.Fatalf("expected cleanup to look for the token Secret under its custom name %q, but it looked for the default name %q instead: %v", customTokenSecret, defaultName, err)
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
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   testBucket,
		AWS:      &forgev1alpha1.AWSStorageSpec{CredentialsSecretRef: testMissingCredsSecret},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	provider := string(forgev1alpha1.ProviderAWSS3)
	forgemetrics.FinalizerCleanupTotal.Reset()
	forgemetrics.StorageReady.Reset()
	forgemetrics.StorageReady.WithLabelValues(app.Namespace, app.Name, provider).Set(1)

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

	// The manager-creation failure here is a k8s NotFound (missing Secret),
	// not an AWS SDK error, so it classifies as other_error rather than
	// timeout/access_denied/not_owned.
	if got := testutil.ToFloat64(forgemetrics.FinalizerCleanupTotal.WithLabelValues(provider, outcomeOther)); got != 1 {
		t.Fatalf("expected FinalizerCleanupTotal{outcome=other_error} to be 1, got %v", got)
	}
	// Cleanup failed, so the Application isn't actually gone yet -- its
	// StorageReady gauge series must stay in place, not be deleted.
	if got := testutil.CollectAndCount(forgemetrics.StorageReady); got != 1 {
		t.Fatalf("expected StorageReady gauge series to survive a failed cleanup, got %d series", got)
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
		SecretName:     testMissingCredsSecret,
		DeletionPolicy: forgev1alpha1.DeletionPolicyRetain,
		// A real AWS manager fails to construct on this missing Secret --
		// deliberately deterministic, unlike an empty SecretName: that
		// falls through to config.LoadDefaultConfig, which succeeds
		// regardless of environment (no error until a real API call), so
		// whether the *subsequent* cleanupAppIRSA call inside
		// cleanupRetainedStorageCredentials succeeds or fails would depend
		// on whatever ambient AWS credentials happen to be sitting around
		// wherever this test runs -- confirmed the hard way: it silently
		// passed against a real logged-in AWS CLI session locally, then
		// failed in CI (no ambient credentials there) with an extra
		// IRSACleanupFailed Event neither environment could agree on. A
		// missing Secret fails at the fake client lookup instead, the same
		// in any environment, so this test only proves what it says: no
		// cloud call is ever reached because manager construction itself
		// fails. See TestFinalizeApplication_RetainStillAttemptsCredentialCleanup
		// below for proof that credential cleanup is actually attempted
		// (and tolerates failure) when construction *does* succeed.
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	rec := &fakeEventRecorder{}
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme, Recorder: rec}

	forgemetrics.StorageReady.Reset()
	forgemetrics.StorageReady.WithLabelValues(app.Namespace, app.Name, string(forgev1alpha1.ProviderAWSS3)).Set(1)

	if err := r.finalizeApplication(context.Background(), app); err != nil {
		t.Fatalf("expected nil error when retaining storage, got %v", err)
	}

	// Retain still means the Application itself is going away -- only the
	// cloud bucket is left alone -- so its StorageReady gauge series must be
	// dropped, same as the Delete path.
	if got := testutil.CollectAndCount(forgemetrics.StorageReady); got != 0 {
		t.Fatalf("expected StorageReady gauge series to be deleted after a successful Retain finalize, got %d series", got)
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

	// An Akamai manager also fails to construct here (no token Secret) --
	// same reasoning as the AWS case above: proves that failure is
	// tolerated silently, not that no cloud-related attempt was made at
	// all.
	if err := r.finalizeApplication(context.Background(), app); err != nil {
		t.Fatalf("expected nil error when retaining storage, got %v", err)
	}
}

func TestFinalizeApplication_RetainStillAttemptsCredentialCleanup(t *testing.T) {
	// The actual behavior cleanupRetainedStorageCredentials adds: Retain
	// protects the bucket, not the credential that happened to reach it.
	// Confirmed here with a token Secret present (so manager construction
	// succeeds, unlike the two tests above) -- finalization must still
	// succeed even though the subsequent real network call (no live Akamai
	// account reachable in this test) fails, proving credential cleanup is
	// best-effort and never blocks the Application's own deletion.
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:       forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:         testBucket,
		DeletionPolicy: forgev1alpha1.DeletionPolicyRetain,
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: app.Name + "-akamai-token", Namespace: testNamespace},
		Data:       map[string][]byte{"apiToken": []byte("token-value")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app, tokenSecret).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.finalizeApplication(context.Background(), app); err != nil {
		t.Fatalf("expected nil error even when the real credential-cleanup network call fails, got %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	cond := findAppCondition(got, "StorageReady")
	if cond == nil || cond.Reason != storagestatus.ReasonBucketRetained {
		t.Fatalf("expected StorageReady reason %q despite the credential cleanup failure, got %#v", storagestatus.ReasonBucketRetained, cond)
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

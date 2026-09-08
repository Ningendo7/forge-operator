package controller

import (
	"context"
	"errors"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	"github.com/Ningendo7/forge-operator/internal/controller/storagestatus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// --- desiredStorage ---

func TestDesiredStorage_ReturnsNilWhenStorageIsNil(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	if got := r.desiredStorage(app, nil); got != nil {
		t.Fatalf("expected nil secret when storage spec is nil, got %#v", got)
	}
}

func TestDesiredStorage_UsesDefaultName(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   testBucket,
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app, nil)

	if secret.Name != testStorageSecretName {
		t.Fatalf("expected default secret name demo-app-storage, got %q", secret.Name)
	}
}

func TestDesiredStorage_UsesConfiguredName(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     testBucket,
		SecretName: "custom-storage-secret",
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app, nil)

	if secret.Name != "custom-storage-secret" {
		t.Fatalf("expected configured secret name, got %q", secret.Name)
	}
}

func TestDesiredStorage_PopulatesBasicFields(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   testBucket,
		Region:   testWestRegion,
		Endpoint: "s3.us-west-2.amazonaws.com",
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app, nil)

	if secret.StringData["provider"] != string(forgev1alpha1.ProviderAWSS3) {
		t.Errorf("expected provider %q, got %q", forgev1alpha1.ProviderAWSS3, secret.StringData["provider"])
	}
	if secret.StringData["bucket"] != testBucket {
		t.Errorf("expected bucket demo-bucket, got %q", secret.StringData["bucket"])
	}
	if secret.StringData["region"] != testWestRegion {
		t.Errorf("expected region us-west-2, got %q", secret.StringData["region"])
	}
	if secret.StringData["endpoint"] != "s3.us-west-2.amazonaws.com" {
		t.Errorf("expected endpoint to be set, got %q", secret.StringData["endpoint"])
	}
	if secret.Labels["app"] != app.Name {
		t.Errorf("expected label 'app' to be %q, got %q", app.Name, secret.Labels["app"])
	}
}

func TestDesiredStorage_InjectsAWSRoleARNFromStatus(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		AWS: &forgev1alpha1.AWSStorageStatus{RoleARN: testRoleARN},
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app, nil)

	if secret.StringData["role_arn"] != testRoleARN {
		t.Fatalf("expected role_arn to be injected from status, got %q", secret.StringData["role_arn"])
	}
}

func TestDesiredStorage_InjectsAkamaiCredentialsFromCaller(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}
	akamaiCreds := &akamaiobjstr.StorageResult{
		AccessKey: testAccessKey,
		SecretKey: testAkamaiSecretKey,
		Endpoint:  testAkamaiEndpoint,
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app, akamaiCreds)

	if secret.StringData["access_key"] != testAccessKey {
		t.Errorf("expected access_key to be injected, got %q", secret.StringData["access_key"])
	}
	if secret.StringData["secret_key"] != testAkamaiSecretKey {
		t.Errorf("expected secret_key to be injected, got %q", secret.StringData["secret_key"])
	}
	if secret.StringData["endpoint"] != testAkamaiEndpoint {
		t.Errorf("expected endpoint to be overridden from Akamai creds, got %q", secret.StringData["endpoint"])
	}
}

func TestDesiredStorage_EndpointURLStripsBucketPrefixForStandardSDKUse(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}
	// resolveEndpoint (Akamai-Obj-Str/obj_storage.go) prefers the bucket's
	// own real hostname, which Linode returns bucket-prefixed --
	// reproducing that shape here, not the bare-cluster-host shape
	// testAkamaiEndpoint happens to already be in.
	akamaiCreds := &akamaiobjstr.StorageResult{
		AccessKey: testAccessKey,
		SecretKey: testAkamaiSecretKey,
		Endpoint:  testBucket + ".us-iad-10.linodeobjects.com",
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app, akamaiCreds)

	// "endpoint" keeps the bucket-prefixed hostname as-is (unchanged,
	// backward-compatible shape) -- only "endpoint_url" (feeding
	// AWS_ENDPOINT_URL) needs the prefix stripped, matching how this
	// operator's own S3 client (s3ClientFor) connects: bare cluster host
	// + path-style addressing, bucket passed explicitly per request. A
	// real SDK handed the bucket-prefixed host as its endpoint would
	// double up the bucket reference the moment it also passes a Bucket
	// parameter, which every normal S3 call does.
	if got, want := secret.StringData["endpoint"], testBucket+".us-iad-10.linodeobjects.com"; got != want {
		t.Errorf("expected endpoint to stay bucket-prefixed, got %q want %q", got, want)
	}
	if got, want := secret.StringData["endpoint_url"], "https://us-iad-10.linodeobjects.com"; got != want {
		t.Errorf("expected endpoint_url to have the bucket prefix stripped, got %q want %q", got, want)
	}
}

func TestDesiredStorage_OmitsSecretKeyWhenAkamaiCredsHaveNone(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}
	akamaiCreds := &akamaiobjstr.StorageResult{AccessKey: testAccessKey}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app, akamaiCreds)

	if _, exists := secret.StringData["secret_key"]; exists {
		t.Fatalf("expected secret_key to be omitted when creds have none, got %q", secret.StringData["secret_key"])
	}
}

// --- reconcileStorageSecret ---

func TestReconcileStorageSecret_CreatesSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorageSecret(context.Background(), app, nil); err != nil {
		t.Fatalf("reconcileStorageSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testStorageSecretName, Namespace: testNamespace}, secret); err != nil {
		t.Fatalf("failed to get storage Secret: %v", err)
	}
	if secret.StringData["bucket"] != testBucket {
		t.Errorf("expected bucket demo-bucket, got %q", secret.StringData["bucket"])
	}
}

func TestReconcileStorageSecret_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorageSecret(context.Background(), app, nil); err != nil {
		t.Fatalf("first reconcileStorageSecret returned error: %v", err)
	}
	if err := r.reconcileStorageSecret(context.Background(), app, nil); err != nil {
		t.Fatalf("second reconcileStorageSecret returned error: %v", err)
	}
}

func TestReconcileStorageSecret_DeletesWhenStorageDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorageSecret(context.Background(), app, nil); err != nil {
		t.Fatalf("failed to create storage secret: %v", err)
	}

	app.Spec.Storage = nil
	if err := r.reconcileStorageSecret(context.Background(), app, nil); err != nil {
		t.Fatalf("reconcileStorageSecret returned error on disable: %v", err)
	}

	secret := &corev1.Secret{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testStorageSecretName, Namespace: testNamespace}, secret)
	if err == nil {
		t.Fatalf("expected storage Secret to be deleted, but it still exists")
	}
}

func TestReconcileStorageSecret_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.UID = "12345"
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorageSecret(context.Background(), app, nil); err != nil {
		t.Fatalf("reconcileStorageSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testStorageSecretName, Namespace: testNamespace}, secret); err != nil {
		t.Fatalf("failed to get storage Secret: %v", err)
	}
	if len(secret.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(secret.OwnerReferences))
	}
}

// --- reconcileStorage dispatch ---

func TestReconcileStorage_NilStorageReconcilesSecretOnly(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileStorage returned error: %v", err)
	}
}

func TestReconcileStorage_SpecRemovalAttemptsCleanupUsingStatusStorage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Spec.Storage is nil (removed), but Status.Storage still remembers a
	// previously-provisioned bucket -- reconcileStorage must attempt real
	// cleanup, not just silently drop the credentials Secret. The missing
	// Secret referenced by SecretName proves the cloud cleanup path was
	// actually entered (manager construction fails), the same technique
	// finalizer_test.go already uses.
	app := newTestApplication()
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     testBucket,
		SecretName: testMissingCredsSecret,
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorage(context.Background(), app); err == nil {
		t.Fatalf("expected an error: cleanup should have been attempted using Status.Storage and failed on the missing credentials Secret")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage == nil {
		t.Fatalf("expected Status.Storage to remain set after a failed cleanup attempt")
	}
	// finalizeApplication must never write a synthesized value onto
	// application.Spec.Storage itself -- only into a local/throwaway copy --
	// or it would leak into any later reconcile step in this same pass that
	// also branches on Spec.Storage (e.g. pod volume/env wiring).
	if app.Spec.Storage != nil {
		t.Fatalf("expected Spec.Storage to remain nil (never mutated in place), got %#v", app.Spec.Storage)
	}
}

func TestReconcileStorage_SpecRemovalWithRetainPolicySkipsCleanupAndClearsStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Provider: forgev1alpha1.ProviderAWSS3,
		Bucket:   testBucket,
		// A real manager would fail to construct on this missing Secret --
		// if Retain actually skips the cloud path as intended, that failure
		// is never reached.
		SecretName:     testMissingCredsSecret,
		DeletionPolicy: forgev1alpha1.DeletionPolicyRetain,
	}
	// Seeded to reproduce a real bug found live: finalizeApplication leaves
	// the StorageReady condition at "cleanup in progress" (BucketCleanup) --
	// harmless on the real deletion path since the whole Application is
	// gone moments later, but here it survives, so without an explicit
	// removal this condition would be stuck lying about state forever.
	apimeta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
		Type:               storagestatus.StorageReady,
		Status:             metav1.ConditionFalse,
		Reason:             storagestatus.ReasonBucketCleanup,
		Message:            "Storage cleanup in progress",
		ObservedGeneration: app.Generation,
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	rec := &fakeEventRecorder{}
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme, Recorder: rec}

	if err := r.reconcileStorage(context.Background(), app); err != nil {
		t.Fatalf("expected no error when deletionPolicy is Retain, got %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage != nil {
		t.Fatalf("expected Status.Storage to be cleared once the operator stops tracking a retained bucket, got %#v", got.Status.Storage)
	}
	if len(rec.events) != 1 || rec.events[0].reason != "StorageRetained" {
		t.Fatalf("expected a single StorageRetained event, got %#v", rec.events)
	}
	if cond := findAppCondition(got, storagestatus.StorageReady); cond != nil {
		t.Fatalf("expected StorageReady condition to be removed once cleanup-on-removal succeeds, still found: %#v", cond)
	}
}

func TestReconcileStorage_ReturnsErrorAndSetsStatusForUnsupportedProvider(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider: "UnknownProvider",
		Bucket:   testBucket,
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	err := r.reconcileStorage(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error for unsupported storage provider, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if len(got.Status.Conditions) == 0 {
		t.Fatalf("expected a status condition to be set for unsupported provider")
	}
}

func TestReconcileStorage_PropagatesAWSReconcileError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     testBucket,
		SecretName: testMissingCredsSecret,
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error when AWS storage manager creation fails, got nil")
	}
}

func TestReconcileStorage_PropagatesAkamaiReconcileError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:   testBucket,
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error when Akamai storage manager creation fails, got nil")
	}
}

// --- findApplicationsForSecret ---

func TestFindApplicationsForSecret_MatchesReferencingApplications(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	matching := newTestApplication()
	matching.Name = "matching-app"
	matching.Spec.Storage = &forgev1alpha1.StorageSpec{SecretName: testSharedCredsSecret}

	nonMatching := newTestApplication()
	nonMatching.Name = "non-matching-app"
	nonMatching.Spec.Storage = &forgev1alpha1.StorageSpec{SecretName: "other-creds"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(matching, nonMatching).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testSharedCredsSecret, Namespace: testNamespace}}
	requests := r.findApplicationsForSecret(context.Background(), secret)

	if len(requests) != 1 {
		t.Fatalf("expected 1 matching request, got %d", len(requests))
	}
	if requests[0].Name != "matching-app" {
		t.Errorf("expected matching-app to be requeued, got %q", requests[0].Name)
	}
}

func TestFindApplicationsForSecret_ReturnsNilForNonSecretObject(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	requests := r.findApplicationsForSecret(context.Background(), newTestApplication())
	if requests != nil {
		t.Fatalf("expected nil requests for non-Secret object, got %v", requests)
	}
}

func TestFindApplicationsForSecret_ReturnsEmptyWhenNoApplicationReferencesSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{SecretName: "other-creds"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testSharedCredsSecret, Namespace: testNamespace}}
	requests := r.findApplicationsForSecret(context.Background(), secret)

	if len(requests) != 0 {
		t.Fatalf("expected no requests, got %d", len(requests))
	}
}

// --- reconcileAWSStorage ---

func withS3StorageManager(t *testing.T, m *mockS3StorageManager) {
	t.Helper()
	original := newS3StorageManager
	newS3StorageManager = func(
		ctx context.Context,
		c client.Client,
		application *forgev1alpha1.Application,
		serviceAccountName string,
		oidcProviderARN string,
		oidcProviderURL string,
	) (s3StorageManager, error) {
		return m, nil
	}
	t.Cleanup(func() { newS3StorageManager = original })
}

func withFailingS3StorageManagerConstruction(t *testing.T, constructErr error) {
	t.Helper()
	original := newS3StorageManager
	newS3StorageManager = func(
		ctx context.Context,
		c client.Client,
		application *forgev1alpha1.Application,
		serviceAccountName string,
		oidcProviderARN string,
		oidcProviderURL string,
	) (s3StorageManager, error) {
		return nil, constructErr
	}
	t.Cleanup(func() { newS3StorageManager = original })
}

func TestReconcileAWSStorage_SetsReadyStatusAndAnnotatesServiceAccountOnSuccess(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket, Region: testWestRegion}

	withS3StorageManager(t, &mockS3StorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*s3storage.StorageResult, error) {
			return &s3storage.StorageResult{RoleARN: testRoleARN}, nil
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	provider := string(forgev1alpha1.ProviderAWSS3)
	forgemetrics.StorageReconcileTotal.Reset()
	forgemetrics.StorageReady.Reset()

	if err := r.reconcileAWSStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileAWSStorage returned error: %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage == nil || got.Status.Storage.AWS == nil || got.Status.Storage.AWS.RoleARN != testRoleARN {
		t.Fatalf("expected status.Storage.AWS.RoleARN to be set, got %#v", got.Status.Storage)
	}
	if got.Status.Storage.Bucket != testBucket || got.Status.Storage.Region != testWestRegion {
		t.Fatalf("expected bucket/region to be recorded, got %#v", got.Status.Storage)
	}

	sa := &corev1.ServiceAccount{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testSAName, Namespace: testNamespace}, sa); err != nil {
		t.Fatalf("expected ServiceAccount to be created/annotated: %v", err)
	}
	if sa.Annotations["eks.amazonaws.com/role-arn"] != testRoleARN {
		t.Fatalf("expected IRSA annotation to be set, got %v", sa.Annotations)
	}

	if got := testutil.ToFloat64(forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeReady)); got != 1 {
		t.Fatalf("expected StorageReconcileTotal{outcome=ready} to be 1, got %v", got)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageReady.WithLabelValues(testNamespace, testAppName, provider)); got != 1 {
		t.Fatalf("expected StorageReady gauge to be 1, got %v", got)
	}
}

func TestReconcileAWSStorage_RecordsSecretNameAndDeletionPolicyInStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:       forgev1alpha1.ProviderAWSS3,
		Bucket:         testBucket,
		SecretName:     testSharedCredsSecret,
		DeletionPolicy: forgev1alpha1.DeletionPolicyRetain,
	}

	withS3StorageManager(t, &mockS3StorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*s3storage.StorageResult, error) {
			return &s3storage.StorageResult{}, nil
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileAWSStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileAWSStorage returned error: %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	// These are what make cleanup possible after spec.storage is later
	// removed -- Status.Storage is all that survives at that point.
	if got.Status.Storage.SecretName != testSharedCredsSecret {
		t.Errorf("expected SecretName to be recorded in status, got %q", got.Status.Storage.SecretName)
	}
	if got.Status.Storage.DeletionPolicy != forgev1alpha1.DeletionPolicyRetain {
		t.Errorf("expected DeletionPolicy to be recorded in status, got %q", got.Status.Storage.DeletionPolicy)
	}
}

func TestReconcileAWSStorage_SkipsIRSAAnnotationWhenRoleARNEmpty(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	withS3StorageManager(t, &mockS3StorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*s3storage.StorageResult, error) {
			return &s3storage.StorageResult{}, nil
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileAWSStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileAWSStorage returned error: %v", err)
	}

	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testSAName, Namespace: testNamespace}, &corev1.ServiceAccount{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no ServiceAccount to be created when RoleARN is empty, got err=%v", err)
	}
}

func TestReconcileAWSStorage_SetsNotReadyWhenManagerConstructionFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	withFailingS3StorageManagerConstruction(t, errors.New("boom"))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileAWSStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error from reconcileAWSStorage, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	cond := findAppCondition(got, "StorageReady")
	if cond == nil || cond.Status != testConditionFalse {
		t.Fatalf("expected StorageReady=False, got %#v", cond)
	}
}

func TestReconcileAWSStorage_SetsNotReadyWhenReconcileBucketFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	withS3StorageManager(t, &mockS3StorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*s3storage.StorageResult, error) {
			return nil, errors.New("bucket reconcile failed")
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	provider := string(forgev1alpha1.ProviderAWSS3)
	forgemetrics.StorageReconcileTotal.Reset()
	forgemetrics.StorageReady.Reset()

	if err := r.reconcileAWSStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error from reconcileAWSStorage, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	cond := findAppCondition(got, "StorageReady")
	if cond == nil || cond.Status != testConditionFalse {
		t.Fatalf("expected StorageReady=False, got %#v", cond)
	}

	if got := testutil.ToFloat64(forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeOther)); got != 1 {
		t.Fatalf("expected StorageReconcileTotal{outcome=other_error} to be 1, got %v", got)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageReady.WithLabelValues(testNamespace, testAppName, provider)); got != 0 {
		t.Fatalf("expected StorageReady gauge to be 0, got %v", got)
	}
}

func TestReconcileAWSStorage_SetsNotOwnedReasonWhenBucketNotOwned(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	withS3StorageManager(t, &mockS3StorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*s3storage.StorageResult, error) {
			return nil, s3storage.ErrBucketNotOwned
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	provider := string(forgev1alpha1.ProviderAWSS3)
	forgemetrics.StorageReconcileTotal.Reset()
	forgemetrics.StorageReady.Reset()

	if err := r.reconcileAWSStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error from reconcileAWSStorage, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	cond := findAppCondition(got, "StorageReady")
	if cond == nil || cond.Status != testConditionFalse {
		t.Fatalf("expected StorageReady=False, got %#v", cond)
	}
	if cond.Reason != storagestatus.ReasonBucketNotOwned {
		t.Fatalf("expected reason %q, got %q", storagestatus.ReasonBucketNotOwned, cond.Reason)
	}

	// Guards the outcome := classifyAWSStorageError(...); if outcome ==
	// outcomeNotOwned refactor: it must still route ErrBucketNotOwned to
	// SetNotOwned (checked above) *and* record the same not_owned outcome
	// on the metric, not silently fall through to other_error.
	if got := testutil.ToFloat64(forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeNotOwned)); got != 1 {
		t.Fatalf("expected StorageReconcileTotal{outcome=not_owned} to be 1, got %v", got)
	}
}

func TestReconcileAWSStorage_PropagatesIRSAAnnotationError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: testBucket}

	withS3StorageManager(t, &mockS3StorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*s3storage.StorageResult, error) {
			return &s3storage.StorageResult{RoleARN: testRoleARN}, nil
		},
	})

	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: &failingPatchClient{Client: baseClient}, Scheme: scheme}

	if err := r.reconcileAWSStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error when IRSA annotation patch fails, got nil")
	}
}

// --- reconcileAkamaiStorage ---

func withAkamaiStorageManager(t *testing.T, m *mockAkamaiStorageManager) {
	t.Helper()
	original := newAkamaiStorageManager
	newAkamaiStorageManager = func(
		ctx context.Context,
		c client.Client,
		application *forgev1alpha1.Application,
		defaultRegion string,
	) (akamaiStorageManager, error) {
		return m, nil
	}
	t.Cleanup(func() { newAkamaiStorageManager = original })
}

func withFailingAkamaiStorageManagerConstruction(t *testing.T, constructErr error) {
	t.Helper()
	original := newAkamaiStorageManager
	newAkamaiStorageManager = func(
		ctx context.Context,
		c client.Client,
		application *forgev1alpha1.Application,
		defaultRegion string,
	) (akamaiStorageManager, error) {
		return nil, constructErr
	}
	t.Cleanup(func() { newAkamaiStorageManager = original })
}

func TestReconcileAkamaiStorage_SetsReadyStatusOnSuccess(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket, Region: "us-east-1"}

	withAkamaiStorageManager(t, &mockAkamaiStorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
			return &akamaiobjstr.StorageResult{AccessKey: testAccessKey, SecretKey: testAkamaiSecretKey, Endpoint: testAkamaiEndpoint}, nil
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	provider := string(forgev1alpha1.ProviderAkamaiObjectStorage)
	forgemetrics.StorageReconcileTotal.Reset()
	forgemetrics.StorageReady.Reset()

	result, err := r.reconcileAkamaiStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileAkamaiStorage returned error: %v", err)
	}

	// The raw credentials come back to the caller for building the storage
	// Secret, but must never be persisted on Application.status.
	if result.AccessKey != testAccessKey || result.SecretKey != testAkamaiSecretKey || result.Endpoint != testAkamaiEndpoint {
		t.Fatalf("expected returned credentials to be populated, got %#v", result)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage == nil || got.Status.Storage.Akamai == nil {
		t.Fatalf("expected status.Storage.Akamai to be set, got %#v", got.Status.Storage)
	}
	if got.Status.Storage.Akamai.Endpoint != testAkamaiEndpoint {
		t.Fatalf("expected status.Storage.Akamai.Endpoint to be set, got %#v", got.Status.Storage.Akamai)
	}

	if got := testutil.ToFloat64(forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeReady)); got != 1 {
		t.Fatalf("expected StorageReconcileTotal{outcome=ready} to be 1, got %v", got)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageReady.WithLabelValues(testNamespace, testAppName, provider)); got != 1 {
		t.Fatalf("expected StorageReady gauge to be 1, got %v", got)
	}
}

func TestReconcileAkamaiStorage_RecordsSecretNameAndDeletionPolicyInStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:       forgev1alpha1.ProviderAkamaiObjectStorage,
		Bucket:         testBucket,
		SecretName:     testSharedCredsSecret,
		DeletionPolicy: forgev1alpha1.DeletionPolicyRetain,
	}

	withAkamaiStorageManager(t, &mockAkamaiStorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
			return &akamaiobjstr.StorageResult{AccessKey: testAccessKey, SecretKey: testAkamaiSecretKey, Endpoint: testAkamaiEndpoint}, nil
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if _, err := r.reconcileAkamaiStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileAkamaiStorage returned error: %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage.SecretName != testSharedCredsSecret {
		t.Errorf("expected SecretName to be recorded in status, got %q", got.Status.Storage.SecretName)
	}
	if got.Status.Storage.DeletionPolicy != forgev1alpha1.DeletionPolicyRetain {
		t.Errorf("expected DeletionPolicy to be recorded in status, got %q", got.Status.Storage.DeletionPolicy)
	}
}

func TestReconcileAkamaiStorage_RecoversPreviousSecretKeyFromExistingSecretWhenNewResultOmitsIt(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}

	// The secret key is never cached on Application.status (see
	// AkamaiStorageStatus's doc comment); it's recovered from the storage
	// Secret this controller previously wrote.
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testStorageSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{"secret_key": []byte("previously-issued-secret")},
	}

	withAkamaiStorageManager(t, &mockAkamaiStorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
			// Akamai only returns the secret key once at creation time.
			return &akamaiobjstr.StorageResult{AccessKey: testAccessKey, Endpoint: testAkamaiEndpoint}, nil
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app, existingSecret).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	result, err := r.reconcileAkamaiStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileAkamaiStorage returned error: %v", err)
	}
	if result.SecretKey != "previously-issued-secret" {
		t.Fatalf("expected previous secret key to be recovered from the existing Secret, got %q", result.SecretKey)
	}
}

func TestReconcileAkamaiStorage_SetsNotReadyWhenManagerConstructionFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}

	withFailingAkamaiStorageManagerConstruction(t, errors.New("boom"))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if _, err := r.reconcileAkamaiStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error from reconcileAkamaiStorage, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	cond := findAppCondition(got, "StorageReady")
	if cond == nil || cond.Status != testConditionFalse {
		t.Fatalf("expected StorageReady=False, got %#v", cond)
	}
}

func TestReconcileAkamaiStorage_SetsNotReadyWhenReconcileBucketFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}

	withAkamaiStorageManager(t, &mockAkamaiStorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
			return nil, errors.New("bucket reconcile failed")
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if _, err := r.reconcileAkamaiStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error from reconcileAkamaiStorage, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	cond := findAppCondition(got, "StorageReady")
	if cond == nil || cond.Status != testConditionFalse {
		t.Fatalf("expected StorageReady=False, got %#v", cond)
	}
}

func TestReconcileAkamaiStorage_SetsNotOwnedReasonWhenBucketNotOwned(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}

	withAkamaiStorageManager(t, &mockAkamaiStorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
			return nil, akamaiobjstr.ErrBucketNotOwned
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	provider := string(forgev1alpha1.ProviderAkamaiObjectStorage)
	forgemetrics.StorageReconcileTotal.Reset()

	if _, err := r.reconcileAkamaiStorage(context.Background(), app); err == nil {
		t.Fatalf("expected error from reconcileAkamaiStorage, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	cond := findAppCondition(got, "StorageReady")
	if cond == nil || cond.Status != testConditionFalse {
		t.Fatalf("expected StorageReady=False, got %#v", cond)
	}
	if cond.Reason != storagestatus.ReasonBucketNotOwned {
		t.Fatalf("expected reason %q, got %q", storagestatus.ReasonBucketNotOwned, cond.Reason)
	}

	// Same refactor guard as the AWS equivalent: outcome ==
	// outcomeNotOwned must still drive both SetNotOwned and the metric.
	if got := testutil.ToFloat64(forgemetrics.StorageReconcileTotal.WithLabelValues(provider, outcomeNotOwned)); got != 1 {
		t.Fatalf("expected StorageReconcileTotal{outcome=not_owned} to be 1, got %v", got)
	}
}

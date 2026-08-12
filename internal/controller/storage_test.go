package controller

import (
	"context"
	"errors"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	akamaiobjstr "github.com/Ningendo7/forge-operator/internal/controller/Akamai-Obj-Str"
	s3storage "github.com/Ningendo7/forge-operator/internal/controller/s3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// --- desiredStorage ---

func TestDesiredStorage_ReturnsNilWhenStorageIsNil(t *testing.T) {
	app := newTestApplication()

	r := &ApplicationReconciler{}
	if got := r.desiredStorage(app); got != nil {
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
	secret := r.desiredStorage(app)

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
	secret := r.desiredStorage(app)

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
	secret := r.desiredStorage(app)

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
	secret := r.desiredStorage(app)

	if secret.StringData["role_arn"] != testRoleARN {
		t.Fatalf("expected role_arn to be injected from status, got %q", secret.StringData["role_arn"])
	}
}

func TestDesiredStorage_InjectsAkamaiCredentialsFromStatus(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Akamai: &forgev1alpha1.AkamaiStorageStatus{
			AccessKey: testAccessKey,
			SecretKey: testAkamaiSecretKey,
			Endpoint:  testAkamaiEndpoint,
		},
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app)

	if secret.StringData["access_key"] != testAccessKey {
		t.Errorf("expected access_key to be injected, got %q", secret.StringData["access_key"])
	}
	if secret.StringData["secret_key"] != testAkamaiSecretKey {
		t.Errorf("expected secret_key to be injected, got %q", secret.StringData["secret_key"])
	}
	if secret.StringData["endpoint"] != testAkamaiEndpoint {
		t.Errorf("expected endpoint to be overridden from Akamai status, got %q", secret.StringData["endpoint"])
	}
}

func TestDesiredStorage_OmitsSecretKeyWhenAkamaiStatusHasNone(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Akamai: &forgev1alpha1.AkamaiStorageStatus{AccessKey: testAccessKey},
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app)

	if _, exists := secret.StringData["secret_key"]; exists {
		t.Fatalf("expected secret_key to be omitted when status has none, got %q", secret.StringData["secret_key"])
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

	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
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

	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
		t.Fatalf("first reconcileStorageSecret returned error: %v", err)
	}
	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
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

	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
		t.Fatalf("failed to create storage secret: %v", err)
	}

	app.Spec.Storage = nil
	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
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

	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
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

func TestReconcileStorage_NoOpProvidersReconcileSecretOnly(t *testing.T) {
	for _, provider := range []string{"MinIO", "minio", providerStatic} {
		t.Run(provider, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = forgev1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			app := newTestApplication()
			app.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.StorageProvider(provider),
				Bucket:   testBucket,
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

			if err := r.reconcileStorage(context.Background(), app); err != nil {
				t.Fatalf("reconcileStorage returned error: %v", err)
			}

			secret := &corev1.Secret{}
			if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testStorageSecretName, Namespace: testNamespace}, secret); err != nil {
				t.Fatalf("expected storage secret to be reconciled for no-op provider: %v", err)
			}
		})
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

	if err := r.reconcileAkamaiStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileAkamaiStorage returned error: %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage == nil || got.Status.Storage.Akamai == nil {
		t.Fatalf("expected status.Storage.Akamai to be set, got %#v", got.Status.Storage)
	}
	akamai := got.Status.Storage.Akamai
	if akamai.AccessKey != testAccessKey || akamai.SecretKey != testAkamaiSecretKey || akamai.Endpoint != testAkamaiEndpoint {
		t.Fatalf("expected Akamai status fields to be populated, got %#v", akamai)
	}
}

func TestReconcileAkamaiStorage_PreservesPreviousSecretKeyWhenNewResultOmitsIt(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: testBucket}
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Akamai: &forgev1alpha1.AkamaiStorageStatus{SecretKey: "previously-issued-secret"},
	}

	withAkamaiStorageManager(t, &mockAkamaiStorageManager{
		reconcileBucketFunc: func(ctx context.Context) (*akamaiobjstr.StorageResult, error) {
			// Akamai only returns the secret key once at creation time.
			return &akamaiobjstr.StorageResult{AccessKey: testAccessKey, Endpoint: testAkamaiEndpoint}, nil
		},
	})

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileAkamaiStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileAkamaiStorage returned error: %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage.Akamai.SecretKey != "previously-issued-secret" {
		t.Fatalf("expected previous secret key to be preserved, got %q", got.Status.Storage.Akamai.SecretKey)
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

	if err := r.reconcileAkamaiStorage(context.Background(), app); err == nil {
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

	if err := r.reconcileAkamaiStorage(context.Background(), app); err == nil {
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

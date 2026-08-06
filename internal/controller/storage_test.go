package controller

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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
		Bucket:   "demo-bucket",
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app)

	if secret.Name != "demo-app-storage" {
		t.Fatalf("expected default secret name demo-app-storage, got %q", secret.Name)
	}
}

func TestDesiredStorage_UsesConfiguredName(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Provider:   forgev1alpha1.ProviderAWSS3,
		Bucket:     "demo-bucket",
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
		Bucket:   "demo-bucket",
		Region:   "us-west-2",
		Endpoint: "s3.us-west-2.amazonaws.com",
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app)

	if secret.StringData["provider"] != string(forgev1alpha1.ProviderAWSS3) {
		t.Errorf("expected provider %q, got %q", forgev1alpha1.ProviderAWSS3, secret.StringData["provider"])
	}
	if secret.StringData["bucket"] != "demo-bucket" {
		t.Errorf("expected bucket demo-bucket, got %q", secret.StringData["bucket"])
	}
	if secret.StringData["region"] != "us-west-2" {
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
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: "demo-bucket"}
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		AWS: &forgev1alpha1.AWSStorageStatus{RoleARN: "arn:aws:iam::123456789012:role/demo-role"},
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app)

	if secret.StringData["role_arn"] != "arn:aws:iam::123456789012:role/demo-role" {
		t.Fatalf("expected role_arn to be injected from status, got %q", secret.StringData["role_arn"])
	}
}

func TestDesiredStorage_InjectsAkamaiCredentialsFromStatus(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: "demo-bucket"}
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Akamai: &forgev1alpha1.AkamaiStorageStatus{
			AccessKey: "access-123",
			SecretKey: "secret-456",
			Endpoint:  "us-east-1.linodeobjects.com",
		},
	}

	r := &ApplicationReconciler{}
	secret := r.desiredStorage(app)

	if secret.StringData["access_key"] != "access-123" {
		t.Errorf("expected access_key to be injected, got %q", secret.StringData["access_key"])
	}
	if secret.StringData["secret_key"] != "secret-456" {
		t.Errorf("expected secret_key to be injected, got %q", secret.StringData["secret_key"])
	}
	if secret.StringData["endpoint"] != "us-east-1.linodeobjects.com" {
		t.Errorf("expected endpoint to be overridden from Akamai status, got %q", secret.StringData["endpoint"])
	}
}

func TestDesiredStorage_OmitsSecretKeyWhenAkamaiStatusHasNone(t *testing.T) {
	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAkamaiObjectStorage, Bucket: "demo-bucket"}
	app.Status.Storage = &forgev1alpha1.StorageStatus{
		Akamai: &forgev1alpha1.AkamaiStorageStatus{AccessKey: "access-123"},
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
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: "demo-bucket"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileStorageSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-storage", Namespace: "default"}, secret); err != nil {
		t.Fatalf("failed to get storage Secret: %v", err)
	}
	if secret.StringData["bucket"] != "demo-bucket" {
		t.Errorf("expected bucket demo-bucket, got %q", secret.StringData["bucket"])
	}
}

func TestReconcileStorageSecret_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: "demo-bucket"}

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
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: "demo-bucket"}

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
	err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-storage", Namespace: "default"}, secret)
	if err == nil {
		t.Fatalf("expected storage Secret to be deleted, but it still exists")
	}
}

func TestReconcileStorageSecret_SetsControllerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApplication()
	app.ObjectMeta.UID = "12345"
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Provider: forgev1alpha1.ProviderAWSS3, Bucket: "demo-bucket"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	if err := r.reconcileStorageSecret(context.Background(), app); err != nil {
		t.Fatalf("reconcileStorageSecret returned error: %v", err)
	}

	secret := &corev1.Secret{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-storage", Namespace: "default"}, secret); err != nil {
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
	for _, provider := range []string{"MinIO", "minio", "Static"} {
		t.Run(provider, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = forgev1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			app := newTestApplication()
			app.Spec.Storage = &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.StorageProvider(provider),
				Bucket:   "demo-bucket",
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

			if err := r.reconcileStorage(context.Background(), app); err != nil {
				t.Fatalf("reconcileStorage returned error: %v", err)
			}

			secret := &corev1.Secret{}
			if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app-storage", Namespace: "default"}, secret); err != nil {
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
		Bucket:   "demo-bucket",
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	err := r.reconcileStorage(context.Background(), app)
	if err == nil {
		t.Fatalf("expected error for unsupported storage provider, got nil")
	}

	got := &forgev1alpha1.Application{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: "default"}, got); err != nil {
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
		Bucket:     "demo-bucket",
		SecretName: "missing-creds",
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
		Bucket:   "demo-bucket",
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
	matching.Spec.Storage = &forgev1alpha1.StorageSpec{SecretName: "shared-creds"}

	nonMatching := newTestApplication()
	nonMatching.Name = "non-matching-app"
	nonMatching.Spec.Storage = &forgev1alpha1.StorageSpec{SecretName: "other-creds"}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(matching, nonMatching).Build()
	r := &ApplicationReconciler{Client: fakeClient, Scheme: scheme}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shared-creds", Namespace: "default"}}
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

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shared-creds", Namespace: "default"}}
	requests := r.findApplicationsForSecret(context.Background(), secret)

	if len(requests) != 0 {
		t.Fatalf("expected no requests, got %d", len(requests))
	}
}

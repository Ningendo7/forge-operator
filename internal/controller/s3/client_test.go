package s3storage

import (
	"context"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewManager_ReturnsErrorWhenStorageSpecIsNil(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com")
	if err == nil {
		t.Fatalf("expected error when storage spec is nil, got nil")
	}
}

func TestNewManager_DefaultsRegionWhenUnset(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com")
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.region != testRegion {
		t.Fatalf("expected default region us-east-1, got %q", manager.region)
	}
	if manager.bucket != testBucket {
		t.Fatalf("expected bucket demo-bucket, got %q", manager.bucket)
	}
}

func TestNewManager_UsesConfiguredRegion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket, Region: testEUWestRegion}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com")
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.region != testEUWestRegion {
		t.Fatalf("expected region eu-west-1, got %q", manager.region)
	}
}

func TestNewManager_ReturnsErrorWhenCredentialsSecretMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Bucket:     testBucket,
		SecretName: "missing-secret",
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com")
	if err == nil {
		t.Fatalf("expected error when credentials secret is missing, got nil")
	}
}

func TestNewManager_ReturnsErrorWhenCredentialsKeysMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Bucket:     testBucket,
		SecretName: testSecretName,
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("id-only")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com")
	if err == nil {
		t.Fatalf("expected error when AWS_SECRET_ACCESS_KEY is missing from secret, got nil")
	}
}

func TestNewManager_SucceedsWithCredentialsSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Bucket:     testBucket,
		SecretName: testSecretName,
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Data: map[string][]byte{
			"AWS_ACCESS_KEY_ID":     []byte("AKIAEXAMPLE"),
			"AWS_SECRET_ACCESS_KEY": []byte("secretexample"),
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com")
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.s3client == nil || manager.iamclient == nil {
		t.Fatalf("expected s3 and iam clients to be initialized")
	}
}

func TestNewManager_PropagatesServiceAccountAndOIDCFields(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, "custom-sa", "arn:oidc:role", "oidc.example.com/id/XYZ")
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.serviceAccountName != "custom-sa" {
		t.Errorf("expected service account name custom-sa, got %q", manager.serviceAccountName)
	}
	if manager.OIDCProviderARN != "arn:oidc:role" {
		t.Errorf("expected OIDCProviderARN arn:oidc:role, got %q", manager.OIDCProviderARN)
	}
	if manager.OIDCProviderURL != "oidc.example.com/id/XYZ" {
		t.Errorf("expected OIDCProviderURL oidc.example.com/id/XYZ, got %q", manager.OIDCProviderURL)
	}
}

package akamaiobjstr

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

	_, err := NewManager(context.Background(), fakeClient, app)
	if err == nil {
		t.Fatalf("expected error when storage spec is nil, got nil")
	}
}

func TestNewManager_ReturnsErrorWhenSecretMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: "demo-bucket"}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app)
	if err == nil {
		t.Fatalf("expected error when credentials secret is missing, got nil")
	}
}

func TestNewManager_ReturnsErrorWhenAPITokenMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: "demo-bucket"}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-storage", Namespace: "default"},
		Data:       map[string][]byte{"other-key": []byte("value")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	_, err := NewManager(context.Background(), fakeClient, app)
	if err == nil {
		t.Fatalf("expected error when apiToken key is missing from secret, got nil")
	}
}

func TestNewManager_UsesDefaultSecretNameAndRegion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: "demo-bucket"}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-app-storage", Namespace: "default"},
		Data:       map[string][]byte{"apiToken": []byte("token123")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	manager, err := NewManager(context.Background(), fakeClient, app)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.bucket != "demo-bucket" {
		t.Errorf("expected bucket demo-bucket, got %q", manager.bucket)
	}
	if manager.region != "us-east-1" {
		t.Errorf("expected default region us-east-1, got %q", manager.region)
	}
}

func TestNewManager_UsesConfiguredSecretNameAndRegion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Bucket:     "demo-bucket",
		Region:     "eu-central",
		SecretName: "custom-creds",
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-creds", Namespace: "default"},
		Data:       map[string][]byte{"apiToken": []byte("token123")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	manager, err := NewManager(context.Background(), fakeClient, app)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.region != "eu-central" {
		t.Errorf("expected region eu-central, got %q", manager.region)
	}
	if manager.akamaiClient == nil {
		t.Fatalf("expected akamai client to be initialized")
	}
}

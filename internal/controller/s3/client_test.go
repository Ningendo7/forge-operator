package s3storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/time/rate"
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

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when storage spec is nil, got nil")
	}
}

func TestNewManager_ReturnsErrorWhenOIDCProviderARNMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "", "oidc.example.com", "arn:boundary", nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when OIDC_PROVIDER_ARN is empty, got nil")
	}
	if !strings.Contains(err.Error(), "OIDC_PROVIDER_ARN") {
		t.Fatalf("expected the error to name OIDC_PROVIDER_ARN, got: %v", err)
	}
}

func TestNewManager_ReturnsErrorWhenOIDCProviderURLMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "", "arn:boundary", nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when OIDC_PROVIDER_URL is empty, got nil")
	}
	if !strings.Contains(err.Error(), "OIDC_PROVIDER_URL") {
		t.Fatalf("expected the error to name OIDC_PROVIDER_URL, got: %v", err)
	}
}

func TestNewManager_ReturnsErrorWhenPermissionsBoundaryARNMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "", nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when APP_IRSA_PERMISSIONS_BOUNDARY_ARN is empty, got nil")
	}
	if !strings.Contains(err.Error(), "APP_IRSA_PERMISSIONS_BOUNDARY_ARN") {
		t.Fatalf("expected the error to name APP_IRSA_PERMISSIONS_BOUNDARY_ARN, got: %v", err)
	}
}

func TestNewManager_DefaultsRegionWhenUnset(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, testLimiter(), testLimiter())
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

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, testLimiter(), testLimiter())
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

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when credentials secret is missing, got nil")
	}
	if !errors.Is(err, ErrCredentialsSecretNotFound) {
		t.Fatalf("expected ErrCredentialsSecretNotFound, got %v", err)
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

	_, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, testLimiter(), testLimiter())
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

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, testLimiter(), testLimiter())
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

	manager, err := NewManager(context.Background(), fakeClient, app, "custom-sa", "arn:oidc:role", "oidc.example.com/id/XYZ", "arn:boundary", nil, testLimiter(), testLimiter())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.serviceAccountName != "custom-sa" {
		t.Errorf("expected service account name custom-sa, got %q", manager.serviceAccountName)
	}
	if manager.OIDCProviderARN != "arn:oidc:role" {
		t.Errorf("expected OIDCProviderARN arn:oidc:role, got %q", manager.OIDCProviderARN)
	}
	if manager.PermissionsBoundaryARN != "arn:boundary" {
		t.Errorf("expected PermissionsBoundaryARN arn:boundary, got %q", manager.PermissionsBoundaryARN)
	}
	if manager.OIDCProviderURL != "oidc.example.com/id/XYZ" {
		t.Errorf("expected OIDCProviderURL oidc.example.com/id/XYZ, got %q", manager.OIDCProviderURL)
	}
}

// TestNewManager_WiresRecordCreatedCallback guards against the exact bug
// found live: NewManager accepted recordCreated as a parameter but never
// assigned it to the returned Manager, so every real bucket creation failed
// at the recordBucketCreated step with "recordCreated callback not
// configured" -- invisible to every other test here since they all pass nil
// for recordCreated (irrelevant to what they're checking) and
// test_helpers_test.go's newTestManager builds the struct directly,
// bypassing this constructor entirely.
func TestNewManager_WiresRecordCreatedCallback(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	called := false
	recordCreated := func(ctx context.Context) error {
		called = true
		return nil
	}

	manager, err := NewManager(context.Background(), fakeClient, app, "custom-sa", "arn:oidc:role", "oidc.example.com/id/XYZ", "arn:boundary", recordCreated, testLimiter(), testLimiter())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.recordCreated == nil {
		t.Fatalf("expected NewManager to wire recordCreated onto the Manager, got nil")
	}
	if err := manager.recordCreated(context.Background()); err != nil {
		t.Fatalf("unexpected error calling the wired recordCreated: %v", err)
	}
	if !called {
		t.Fatalf("expected the wired recordCreated to be the callback passed to NewManager, but it was never invoked")
	}
}

// blockedLimiter never has a token to give (burst 0), so
// rate.Limiter.Wait fails immediately -- deterministically, with no actual
// waiting or network activity -- rather than proceeding. Used below to
// prove which of s3client/iamclient a given limiter was actually attached
// to, without needing a reachable AWS endpoint.
func blockedLimiter() *rate.Limiter {
	return rate.NewLimiter(rate.Limit(1), 0)
}

// TestNewManager_S3LimiterAppliesToS3ClientNotIAMClient guards against the
// s3Limiter/iamLimiter constructor arguments being swapped (or both
// accidentally wired to the same client): with s3Limiter blocked and
// iamLimiter wide open, an S3 call must fail fast with the rate-limit
// error. Because AWSMiddleware runs in the Finalize step (before the
// request is ever sent, see ratelimit.go), this never touches the
// network -- it fails on the same wait rejection whether or not a real
// AWS endpoint is reachable.
func TestNewManager_S3LimiterAppliesToS3ClientNotIAMClient(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, blockedLimiter(), rate.NewLimiter(rate.Inf, 1))
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	_, err = manager.s3client.HeadBucket(context.Background(), &s3sdk.HeadBucketInput{Bucket: aws.String(testBucket)})
	if err == nil {
		t.Fatalf("expected the blocked s3Limiter to reject this call, got nil error")
	}
	if !strings.Contains(err.Error(), "rate limit wait") {
		t.Fatalf("expected a rate-limit-wait error, got: %v", err)
	}
}

// TestNewManager_IAMLimiterAppliesToIAMClientNotS3Client is the mirror of
// the test above: s3Limiter wide open, iamLimiter blocked, so an IAM call
// must fail fast on the rate limiter -- proving iamLimiter reached
// iamclient specifically.
func TestNewManager_IAMLimiterAppliesToIAMClientNotS3Client(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, "demo-app-sa", "arn:oidc", "oidc.example.com", "arn:boundary", nil, rate.NewLimiter(rate.Inf, 1), blockedLimiter())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	_, err = manager.iamclient.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("demo-role"),
		AssumeRolePolicyDocument: aws.String("{}"),
	})
	if err == nil {
		t.Fatalf("expected the blocked iamLimiter to reject this call, got nil error")
	}
	if !strings.Contains(err.Error(), "rate limit wait") {
		t.Fatalf("expected a rate-limit-wait error, got: %v", err)
	}
}

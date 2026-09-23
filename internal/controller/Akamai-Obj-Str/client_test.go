package akamaiobjstr

import (
	"context"
	"errors"
	"strings"
	"testing"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testDefaultTokenSecretName = "demo-app-akamai-token"
	testAPITokenKey            = "apiToken"
)

func TestNewManager_ReturnsErrorWhenStorageSpecIsNil(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app, testRegion, nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when storage spec is nil, got nil")
	}
}

func TestNewManager_ReturnsErrorWhenSecretMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := NewManager(context.Background(), fakeClient, app, testRegion, nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when credentials secret is missing, got nil")
	}
	if !errors.Is(err, ErrTokenSecretNotFound) {
		t.Fatalf("expected ErrTokenSecretNotFound, got %v", err)
	}
}

func TestNewManager_ReturnsErrorWhenAPITokenMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testDefaultTokenSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{"other-key": []byte("value")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	_, err := NewManager(context.Background(), fakeClient, app, testRegion, nil, testLimiter(), testLimiter())
	if err == nil {
		t.Fatalf("expected error when apiToken key is missing from secret, got nil")
	}
}

func TestNewManager_UsesDefaultSecretNameAndRegion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testDefaultTokenSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testAPITokenKey: []byte("token123")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, testRegion, nil, testLimiter(), testLimiter())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.bucket != testBucket {
		t.Errorf("expected bucket demo-bucket, got %q", manager.bucket)
	}
	if manager.region != testRegion {
		t.Errorf("expected the caller-supplied default region %q, got %q", testRegion, manager.region)
	}
}

func TestNewManager_EmptyRegionWhenNeitherSpecNorDefaultIsSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testDefaultTokenSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testAPITokenKey: []byte("token123")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	// No spec.storage.region and no caller-supplied default: region must end
	// up empty rather than silently falling back to some hardcoded guess
	// that may not match wherever this operator is actually deployed.
	manager, err := NewManager(context.Background(), fakeClient, app, "", nil, testLimiter(), testLimiter())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.region != "" {
		t.Errorf("expected empty region when neither spec nor default is set, got %q", manager.region)
	}
}

func TestNewManager_UsesConfiguredSecretNameAndRegion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{
		Bucket: testBucket,
		Region: "eu-central",
		Akamai: &forgev1alpha1.AkamaiStorageSpec{AccessKeySecretRef: "custom-creds"},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-creds", Namespace: testNamespace},
		Data:       map[string][]byte{testAPITokenKey: []byte("token123")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	// A different value than the spec's "eu-central", to prove
	// spec.storage.region takes precedence over the caller-supplied default.
	manager, err := NewManager(context.Background(), fakeClient, app, "operator-default-region", nil, testLimiter(), testLimiter())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if manager.region != "eu-central" {
		t.Errorf("expected spec.storage.region (eu-central) to win over the default, got %q", manager.region)
	}
	if manager.akamaiClient == nil {
		t.Fatalf("expected akamai client to be initialized")
	}
}

func TestNewManager_UsesDistinctDefaultFromOutputStorageSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}

	// A Secret at the *output* default name ("demo-app-storage") exists but
	// holds no apiToken (it's the operator's own generated credentials
	// Secret, not a user-supplied one) — NewManager must not look here.
	outputSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testStorageSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{"access_key": []byte("generated-key")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(outputSecret).Build()

	if _, err := NewManager(context.Background(), fakeClient, app, testRegion, nil, testLimiter(), testLimiter()); err == nil {
		t.Fatalf("expected error: NewManager should not have found an apiToken in the output storage Secret")
	}
}

// blockedLimiter never has a token to give (burst 0), so
// rate.Limiter.Wait fails immediately -- deterministically, with no actual
// waiting or network activity -- rather than proceeding. Used below to
// prove which client a given limiter was actually attached to, without
// needing a reachable Akamai/Linode endpoint.
func blockedLimiter() *rate.Limiter {
	return rate.NewLimiter(rate.Limit(1), 0)
}

// TestNewManager_AccountLimiterAppliesToAccountClientNotObjectClient guards
// against accountLimiter/objectLimiter being swapped: with accountLimiter
// blocked, a call through the account API client (linodego, via
// oauthClient.Transport) must fail fast with the rate-limit error --
// before ever reaching the network, since RoundTripper checks Wait before
// delegating to the real transport (see ratelimit.go).
func TestNewManager_AccountLimiterAppliesToAccountClientNotObjectClient(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := newTestApp()
	app.Spec.Storage = &forgev1alpha1.StorageSpec{Bucket: testBucket}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testDefaultTokenSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testAPITokenKey: []byte("token123")},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	manager, err := NewManager(context.Background(), fakeClient, app, testRegion, nil, blockedLimiter(), testLimiter())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	_, err = manager.akamaiClient.ListObjectStorageBuckets(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected the blocked accountLimiter to reject this call, got nil error")
	}
	if !strings.Contains(err.Error(), "rate limit wait") {
		t.Fatalf("expected a rate-limit-wait error, got: %v", err)
	}
}

// TestNewS3ObjectClient_AppliesObjectLimiter is the object-endpoint mirror
// of the account-client test above: newS3ObjectClient (what s3ClientFor
// builds lazily per-bucket, see client.go) must apply the objectLimiter it
// was given.
func TestNewS3ObjectClient_AppliesObjectLimiter(t *testing.T) {
	client := newS3ObjectClient(testRegion, "us-east-1.linodeobjects.com", "access-key", "secret-key", blockedLimiter())

	_, err := client.GetObject(context.Background(), &s3sdk.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String("marker"),
	})
	if err == nil {
		t.Fatalf("expected the blocked objectLimiter to reject this call, got nil error")
	}
	if !strings.Contains(err.Error(), "rate limit wait") {
		t.Fatalf("expected a rate-limit-wait error, got: %v", err)
	}
}

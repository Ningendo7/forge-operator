//go:build integration

// Package akamaiobjstr's integration tests exercise this Manager against a
// real Akamai/Linode Object Storage account -- no Kubernetes cluster
// involved (the Kubernetes-facing half of Manager is already well covered
// by the fake-client unit tests in this same package; only the cloud-facing
// half has ever gone untested by anything other than manual, ad-hoc live
// cluster sessions). Every real bug found in this package during live
// testing (access-key reuse returning an empty secret on a second
// reconcile, the bucket never being emptied before deletion, ownership
// verification gaps) was invisible to the mocked unit test suite by
// construction: a mock only ever proves the code reacts correctly to what
// the test author assumed the real API returns, never that the assumption
// itself was right. These tests exist to catch exactly that category
// again, automatically, before a human has to find it live.
//
// Skipped entirely unless LINODE_TOKEN is set, so `go test ./...` and CI
// never need real credentials. Run explicitly with:
//
//	LINODE_TOKEN=... go test -tags=integration ./internal/controller/Akamai-Obj-Str/... -v
//
// Every test creates its own uniquely-named real bucket and cleans it (and
// its access key) up via t.Cleanup, which runs even if the test body fails
// partway through -- nothing should ever be left behind in the account.
package akamaiobjstr

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
)

const integrationTestRegion = "us-iad"

// skipUnlessLiveAkamaiCredentials skips the calling test unless real
// credentials are available, so this file is always safe to compile and
// `go test` is always safe to run without them.
func skipUnlessLiveAkamaiCredentials(t *testing.T) string {
	t.Helper()
	token := os.Getenv("LINODE_TOKEN")
	if token == "" {
		t.Skip("LINODE_TOKEN not set -- skipping live Akamai integration test")
	}
	return token
}

// newIntegrationApp builds a fake-client-backed Application for integration
// tests -- the Kubernetes side is still faked (it's already well covered
// elsewhere), only the cloud client underneath the Manager is real. Each
// call gets a fresh random UID and a unique bucket name so concurrent or
// repeated runs never collide.
func newIntegrationApp(t *testing.T, namePrefix string) (*forgev1alpha1.Application, string) {
	t.Helper()

	suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Intn(1_000_000))
	bucket := fmt.Sprintf("forge-integration-test-%s", suffix)

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namePrefix + "-" + suffix,
			Namespace: "default",
			UID:       types.UID(fmt.Sprintf("00000000-0000-0000-0000-%012d", time.Now().UnixNano()%1_000_000_000_000)),
		},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx",
			Storage: &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.ProviderAkamaiObjectStorage,
				Bucket:   bucket,
				Region:   integrationTestRegion,
			},
		},
	}
	return app, bucket
}

// newIntegrationManager builds a real Manager for the given Application,
// backed by a fake Kubernetes client seeded with extraObjects alongside the
// Application itself (used for the adoption test, which needs a second
// Application present to prove adopt-bucket refuses to take over a bucket
// from an owner that still exists).
func newIntegrationManager(t *testing.T, app *forgev1alpha1.Application, extraObjects ...forgev1alpha1.Application) *Manager {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := forgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: naming.AkamaiTokenSecret(app), Namespace: app.Namespace},
		Data:       map[string][]byte{"apiToken": []byte(os.Getenv("LINODE_TOKEN"))},
	}

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app, tokenSecret).WithStatusSubresource(app)
	for i := range extraObjects {
		builder = builder.WithObjects(&extraObjects[i])
	}
	fakeClient := builder.Build()

	m, err := NewManager(context.Background(), fakeClient, app, integrationTestRegion)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	return m
}

// cleanupBucket is the safety-net teardown registered via t.Cleanup --
// separate from a test's own explicit DeleteBucket call in the middle of
// the test, so a real bucket still gets removed even if the test fails or
// panics before reaching that point. Ignores errors: if the bucket's
// already gone (the test's own cleanup succeeded), DeleteBucket already
// tolerates that.
func cleanupBucket(t *testing.T, m *Manager) {
	t.Helper()
	if _, err := m.DeleteBucket(context.Background()); err != nil {
		t.Logf("cleanup: DeleteBucket returned an error (may be pre-existing/already handled): %v", err)
	}
}

func TestIntegration_Akamai_BucketLifecycle_ReconcileRetryStillReturnsUsableSecret(t *testing.T) {
	skipUnlessLiveAkamaiCredentials(t)

	app, _ := newIntegrationApp(t, "akamai-lifecycle")
	m := newIntegrationManager(t, app)
	t.Cleanup(func() { cleanupBucket(t, m) })

	result, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("ReconcileBucket returned error: %v", err)
	}
	if result.AccessKey == "" || result.SecretKey == "" {
		t.Fatalf("expected a usable access key on first reconcile, got AccessKey=%q SecretKey empty=%v", result.AccessKey, result.SecretKey == "")
	}
	firstAccessKey := result.AccessKey

	// This is the exact regression class found live: ensureAccessKey found
	// an existing key by label on a second call and returned an empty
	// SecretKey instead of either reusing a recoverable one or issuing a
	// fresh key -- silently breaking every reconcile after the first for
	// that Application. A real second ReconcileBucket call is the only way
	// to catch this: no mock of "what Linode's key API returns the second
	// time" would have been written to include this bug, since nobody knew
	// to assume it.
	result2, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("second ReconcileBucket (simulating a retry) returned error: %v", err)
	}
	if result2.AccessKey == "" || result2.SecretKey == "" {
		t.Fatalf("expected a usable access key on a second/retry reconcile too, got AccessKey=%q SecretKey empty=%v -- this is the exact access-key-reuse regression found live", result2.AccessKey, result2.SecretKey == "")
	}
	if result2.AccessKey != firstAccessKey {
		t.Logf("note: access key changed between reconciles (%s -> %s) -- acceptable (delete+recreate is a valid recovery path), just confirming it's still usable below", firstAccessKey, result2.AccessKey)
	}

	// Prove the returned credentials are genuinely usable for real object
	// I/O, using the exact same client construction path this Manager uses
	// internally -- this is the actual point of the whole storage feature.
	s3Client := newS3ObjectClient(integrationTestRegion, strings.TrimPrefix(result2.Endpoint, app.Spec.Storage.Bucket+"."), result2.AccessKey, result2.SecretKey)
	body := []byte("forge-operator integration test object")
	if _, err := s3Client.PutObject(context.Background(), &s3sdk.PutObjectInput{
		Bucket: aws.String(app.Spec.Storage.Bucket),
		Key:    aws.String("integration-test-object.txt"),
		Body:   strings.NewReader(string(body)),
	}); err != nil {
		t.Fatalf("PutObject with the reconciled credentials failed: %v", err)
	}
	out, err := s3Client.GetObject(context.Background(), &s3sdk.GetObjectInput{
		Bucket: aws.String(app.Spec.Storage.Bucket),
		Key:    aws.String("integration-test-object.txt"),
	})
	if err != nil {
		t.Fatalf("GetObject with the reconciled credentials failed: %v", err)
	}
	got, err := io.ReadAll(out.Body)
	_ = out.Body.Close()
	if err != nil {
		t.Fatalf("failed to read object body: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("expected object content %q, got %q", body, got)
	}

	// Real bucket-emptying-then-deletion, the other regression class found
	// live (a bucket with real objects in it, not just the ownership
	// marker, previously failed to delete at all).
	if _, err := m.DeleteBucket(context.Background()); err != nil {
		t.Fatalf("DeleteBucket returned error: %v", err)
	}
	if _, err := m.akamaiClient.GetObjectStorageBucket(context.Background(), integrationTestRegion, app.Spec.Storage.Bucket); err == nil {
		t.Fatalf("expected the bucket to be genuinely gone after DeleteBucket, but GetObjectStorageBucket still found it")
	}
}

func TestIntegration_Akamai_OwnershipRejection_SecondApplicationCannotClaim(t *testing.T) {
	skipUnlessLiveAkamaiCredentials(t)

	ownerApp, bucket := newIntegrationApp(t, "akamai-owner")
	owner := newIntegrationManager(t, ownerApp)
	t.Cleanup(func() { cleanupBucket(t, owner) })

	if _, err := owner.ReconcileBucket(context.Background()); err != nil {
		t.Fatalf("owner ReconcileBucket returned error: %v", err)
	}

	intruderApp, _ := newIntegrationApp(t, "akamai-intruder")
	intruderApp.Spec.Storage.Bucket = bucket // point at the same real bucket
	intruder := newIntegrationManager(t, intruderApp)

	_, err := intruder.ReconcileBucket(context.Background())
	if !isBucketNotOwnedErr(err) {
		t.Fatalf("expected ErrBucketNotOwned when a different Application (no adopt annotation) targets an already-owned real bucket, got %v", err)
	}
}

func TestIntegration_Akamai_Adoption_RefusesWhileOwnerStillExists_SucceedsOnceGone(t *testing.T) {
	skipUnlessLiveAkamaiCredentials(t)

	ownerApp, bucket := newIntegrationApp(t, "akamai-adopt-owner")
	owner := newIntegrationManager(t, ownerApp)
	t.Cleanup(func() { cleanupBucket(t, owner) })

	if _, err := owner.ReconcileBucket(context.Background()); err != nil {
		t.Fatalf("owner ReconcileBucket returned error: %v", err)
	}

	adopterApp, _ := newIntegrationApp(t, "akamai-adopter")
	adopterApp.Spec.Storage.Bucket = bucket
	adopterApp.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	// First: the real owner Application object still exists (seeded
	// alongside the adopter in the fake Kubernetes client) -- adoption
	// must be refused even against a real bucket/marker, exactly the live
	// takeover this check exists to close.
	stillAlive := newIntegrationManager(t, adopterApp, *ownerApp)
	if _, err := stillAlive.ReconcileBucket(context.Background()); !isBucketNotOwnedErr(err) {
		t.Fatalf("expected adoption to be refused while the owner Application still exists, got %v", err)
	}

	// Now without the owner present in the (fake) cluster at all --
	// simulating it having actually been deleted -- adoption of the same
	// real bucket must succeed.
	gone := newIntegrationManager(t, adopterApp)
	result, err := gone.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("expected adoption to succeed once the owner Application no longer exists, got error: %v", err)
	}
	if result.AccessKey == "" {
		t.Fatalf("expected a usable access key after a successful adoption")
	}

	t.Cleanup(func() { cleanupBucket(t, gone) })
}

func isBucketNotOwnedErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrBucketNotOwned.Error())
}

//go:build integration

// Package s3storage's integration tests exercise this Manager against a
// real AWS account -- no Kubernetes cluster involved (see the identical
// reasoning in the Akamai-Obj-Str package's own integration_test.go; the
// Kubernetes-facing half of Manager is already well covered by the
// fake-client unit tests in this package, only the cloud-facing half has
// ever gone untested by anything but manual, ad-hoc live sessions).
//
// Unlike Akamai, this operator never generates a static AWS credential
// pair for an Application to use -- AWS storage is IRSA-based, and IRSA's
// whole point is that the Application's own pod gets temporary credentials
// transparently via a real EKS OIDC-federated ServiceAccount token, which
// a local Go test process has no way to obtain (there's no live pod, no
// real OIDC federation to assume the role through). So these tests verify
// what's actually reachable and meaningful from here: the bucket and IAM
// role/policy this Manager creates genuinely exist with the right shape,
// retrying is safe, and ownership/adoption behave the same way they do for
// Akamai -- not that the generated IRSA role can itself be assumed, which
// only a real EKS pod can ever prove.
//
// Skipped entirely unless real AWS credentials are reachable via the
// standard credential chain (the same one this package's own NewManager
// uses, and the same one your AWS Terraform provider already relies on --
// see providers.tf, which sets no explicit credentials block), so
// `go test ./...` and CI never need them. Run explicitly with whatever
// already authenticates your AWS CLI/Terraform active in the shell:
//
//	go test -tags=integration ./internal/controller/s3/... -v
//
// Every test creates its own uniquely-named real bucket (and IAM role) and
// cleans both up via t.Cleanup, which runs even if the test body fails
// partway through.
package s3storage

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
)

const (
	integrationTestRegion          = "us-east-1"
	integrationTestOIDCProviderARN = "arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"
	integrationTestOIDCProviderURL = "oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"
)

// skipUnlessLiveAWSCredentials skips the calling test unless the standard
// AWS credential chain actually resolves to something real -- checked with
// a genuine, harmless STS call (GetCallerIdentity needs no permissions
// beyond being a valid, authenticated principal) rather than looking for
// any specific env var, since credentials might come from a shared file, an
// instance role, or anything else the chain supports.
func skipUnlessLiveAWSCredentials(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(integrationTestRegion))
	if err != nil {
		t.Skipf("no AWS config available -- skipping live AWS integration test: %v", err)
	}
	if _, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err != nil {
		t.Skipf("AWS credentials not usable -- skipping live AWS integration test: %v", err)
	}
}

func newIntegrationApp(t *testing.T, namePrefix string) *forgev1alpha1.Application {
	t.Helper()
	suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Intn(1_000_000))
	bucket := fmt.Sprintf("forge-integration-test-%s", suffix)

	return &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namePrefix + "-" + suffix,
			Namespace: "default",
			UID:       types.UID(fmt.Sprintf("00000000-0000-0000-0000-%012d", time.Now().UnixNano()%1_000_000_000_000)),
		},
		Spec: forgev1alpha1.ApplicationSpec{
			Image: "nginx",
			Storage: &forgev1alpha1.StorageSpec{
				Provider: forgev1alpha1.ProviderAWSS3,
				Bucket:   bucket,
				Region:   integrationTestRegion,
			},
		},
	}
}

func newIntegrationManager(t *testing.T, app *forgev1alpha1.Application, extraObjects ...forgev1alpha1.Application) *Manager {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := forgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app)
	for i := range extraObjects {
		builder = builder.WithObjects(&extraObjects[i])
	}
	fakeClient := builder.Build()

	m, err := NewManager(context.Background(), fakeClient, app, app.Name+"-sa", integrationTestOIDCProviderARN, integrationTestOIDCProviderURL)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	return m
}

// cleanup is the t.Cleanup safety net -- separate from a test's own
// explicit CleanupBucket call partway through, so real AWS resources still
// get removed if the test fails or panics before reaching that point.
func cleanup(t *testing.T, m *Manager) {
	t.Helper()
	if irsaErr, err := m.CleanupBucket(context.Background()); err != nil || irsaErr != nil {
		t.Logf("cleanup: CleanupBucket returned an error (may be pre-existing/already handled): err=%v irsaErr=%v", err, irsaErr)
	}
}

func TestIntegration_AWS_BucketLifecycle_RetryIsSafeAndIAMRoleIsReal(t *testing.T) {
	skipUnlessLiveAWSCredentials(t)

	app := newIntegrationApp(t, "aws-lifecycle")
	m := newIntegrationManager(t, app)
	t.Cleanup(func() { cleanup(t, m) })

	result, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("ReconcileBucket returned error: %v", err)
	}
	if result.RoleARN == "" {
		t.Fatalf("expected a non-empty RoleARN on first reconcile")
	}
	firstRoleARN := result.RoleARN

	// The AWS equivalent of the Akamai retry regression: a second
	// reconcile (simulating an ordinary workqueue retry) must remain
	// idempotent -- same role, no duplicate-resource errors -- rather than
	// erroring out or silently drifting.
	result2, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("second ReconcileBucket (simulating a retry) returned error: %v", err)
	}
	if result2.RoleARN != firstRoleARN {
		t.Fatalf("expected the same RoleARN on a retry, got %q then %q", firstRoleARN, result2.RoleARN)
	}

	// Confirm the IAM role this Manager reports actually exists as a real
	// AWS resource, not just that the SDK call reported no error.
	roleName := m.irsaRoleName()
	if _, err := m.iamclient.GetRole(context.Background(), &iam.GetRoleInput{RoleName: aws.String(roleName)}); err != nil {
		t.Fatalf("expected IAM role %q to exist after ReconcileBucket, GetRole failed: %v", roleName, err)
	}

	if irsaErr, err := m.CleanupBucket(context.Background()); err != nil {
		t.Fatalf("CleanupBucket returned error: %v (irsaErr=%v)", err, irsaErr)
	}
	if _, err := m.iamclient.GetRole(context.Background(), &iam.GetRoleInput{RoleName: aws.String(roleName)}); err == nil {
		t.Fatalf("expected IAM role %q to be gone after CleanupBucket", roleName)
	}
}

func TestIntegration_AWS_OwnershipRejection_SecondApplicationCannotClaim(t *testing.T) {
	skipUnlessLiveAWSCredentials(t)

	ownerApp := newIntegrationApp(t, "aws-owner")
	owner := newIntegrationManager(t, ownerApp)
	t.Cleanup(func() { cleanup(t, owner) })

	if _, err := owner.ReconcileBucket(context.Background()); err != nil {
		t.Fatalf("owner ReconcileBucket returned error: %v", err)
	}

	intruderApp := newIntegrationApp(t, "aws-intruder")
	intruderApp.Spec.Storage.Bucket = ownerApp.Spec.Storage.Bucket // same real bucket
	intruder := newIntegrationManager(t, intruderApp)

	if _, err := intruder.ReconcileBucket(context.Background()); !isBucketNotOwnedErr(err) {
		t.Fatalf("expected ErrBucketNotOwned when a different Application (no adopt annotation) targets an already-owned real bucket, got %v", err)
	}
}

func TestIntegration_AWS_Adoption_RefusesWhileOwnerStillExists_SucceedsOnceGone(t *testing.T) {
	skipUnlessLiveAWSCredentials(t)

	ownerApp := newIntegrationApp(t, "aws-adopt-owner")
	owner := newIntegrationManager(t, ownerApp)
	t.Cleanup(func() { cleanup(t, owner) })

	if _, err := owner.ReconcileBucket(context.Background()); err != nil {
		t.Fatalf("owner ReconcileBucket returned error: %v", err)
	}

	adopterApp := newIntegrationApp(t, "aws-adopter")
	adopterApp.Spec.Storage.Bucket = ownerApp.Spec.Storage.Bucket
	adopterApp.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	stillAlive := newIntegrationManager(t, adopterApp, *ownerApp)
	if _, err := stillAlive.ReconcileBucket(context.Background()); !isBucketNotOwnedErr(err) {
		t.Fatalf("expected adoption to be refused while the owner Application still exists, got %v", err)
	}

	gone := newIntegrationManager(t, adopterApp)
	result, err := gone.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("expected adoption to succeed once the owner Application no longer exists, got error: %v", err)
	}
	if result.RoleARN == "" {
		t.Fatalf("expected a usable RoleARN after a successful adoption")
	}
	t.Cleanup(func() { cleanup(t, gone) })
}

func isBucketNotOwnedErr(err error) bool {
	return errors.Is(err, ErrBucketNotOwned)
}

package s3storage

import (
	"context"
	"errors"
	"testing"
	"time"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// --- ensureBucketExists ---

// matchingTagging returns a GetBucketTaggingOutput carrying the ownership
// tag (and matching namespace tag) for testAppUID, i.e. what
// claimOrVerifyOwnership sees for a bucket this operator created for this
// Application, already fully tagged under namespace-scoped ownership.
func matchingTagging() *s3sdk.GetBucketTaggingOutput {
	return &s3sdk.GetBucketTaggingOutput{
		TagSet: []s3types.Tag{
			{Key: aws.String(ownerTagKey), Value: aws.String(string(testAppUID))},
			{Key: aws.String(ownerNamespaceTagKey), Value: aws.String(testNamespace)},
		},
	}
}

func TestEnsureBucketExists_OwnedBucketFound(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return matchingTagging(), nil
		},
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			createCalled = true
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected CreateBucket not to be called when bucket already exists")
	}
}

func TestEnsureBucketExists_ReturnsNotOwnedWhenFoundBucketTagMismatched(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
				},
			}, nil
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestEnsureBucketExists_AdoptsMismatchedTagWhenAnnotationSet(t *testing.T) {
	var taggedOwner, taggedNamespace string
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
					{Key: aws.String(ownerNamespaceTagKey), Value: aws.String(testNamespace)},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			for _, tag := range params.Tagging.TagSet {
				switch aws.ToString(tag.Key) {
				case ownerTagKey:
					taggedOwner = aws.ToString(tag.Value)
				case ownerNamespaceTagKey:
					taggedNamespace = aws.ToString(tag.Value)
				}
			}
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	// A tag naming a *different* Application must still be adoptable via the
	// explicit annotation, within the same namespace as the previous owner
	// (testNamespace here) -- this is the deliberate human-in-the-loop
	// override, distinct from (and not gated by) previouslyCreatedByUs,
	// which by definition can never be true for a bucket someone else made.
	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if taggedOwner != string(testAppUID) {
		t.Fatalf("expected the tag to be overwritten with this Application's own UID, got %q", taggedOwner)
	}
	if taggedNamespace != testNamespace {
		t.Fatalf("expected the owner-namespace tag to be set to this Application's namespace, got %q", taggedNamespace)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 1 {
		t.Fatalf("expected StorageBucketAdoptedTotal to be 1 after an actual adoption, got %v", got)
	}
}

func TestEnsureBucketExists_RefusesToAdoptWhenPreviousOwnerStillExists(t *testing.T) {
	putTaggingCalled := false
	s3Client := &mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTaggingCalled = true
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testNamespace, UID: testAppUID},
	}
	app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}
	// The Application actually named by the tag's UID is still very much
	// alive -- adopt-bucket must not be able to take its bucket away from
	// it just because the annotation is set.
	stillAliveOwner := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "still-alive-owner", Namespace: testNamespace, UID: testOtherUID},
	}

	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app, stillAliveOwner).WithStatusSubresource(app).Build()

	m := &Manager{
		k8sClient: fakeClient,
		s3client:  s3Client,
		app:       app,
		storage:   &forgev1alpha1.StorageSpec{Bucket: testBucket, Region: testRegion},
		bucket:    testBucket,
		region:    testRegion,
	}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned when the previous owner still exists, got %v", err)
	}
	if putTaggingCalled {
		t.Fatalf("expected the tag to be left untouched when the previous owner still exists")
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 0 {
		t.Fatalf("expected no adoption to be recorded, got %v", got)
	}
}

func TestEnsureBucketExists_BackfillsNamespaceTagForBucketAlreadyOwned(t *testing.T) {
	// A bucket tagged before namespace-scoped ownership shipped carries the
	// owner tag but no owner-namespace tag. Since the UID already matches
	// this Application, that's safe to self-heal by rewriting the tags --
	// no adoption semantics involved, just catching the record up.
	var putTagSet []s3types.Tag
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testAppUID))},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTagSet = params.Tagging.TagSet
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}

	var gotNamespace string
	for _, tag := range putTagSet {
		if aws.ToString(tag.Key) == ownerNamespaceTagKey {
			gotNamespace = aws.ToString(tag.Value)
		}
	}
	if gotNamespace != testNamespace {
		t.Fatalf("expected the owner-namespace tag to be backfilled to %q, got %q", testNamespace, gotNamespace)
	}
}

func TestEnsureBucketExists_DoesNotRewriteTagsWhenAlreadyFullyTagged(t *testing.T) {
	putTaggingCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return matchingTagging(), nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTaggingCalled = true
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if putTaggingCalled {
		t.Fatalf("expected no tag write when the bucket is already fully tagged for this Application")
	}
}

func TestEnsureBucketExists_BackfillsOwnershipIDTagForBucketAlreadyOwned(t *testing.T) {
	// Same backfill story as the namespace tag, for a bucket tagged before
	// the ownership-ID tag existed at all.
	var putTagSet []s3types.Tag
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return matchingTagging(), nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTagSet = params.Tagging.TagSet
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Annotations = map[string]string{naming.StorageOwnershipIDAnnotation: testOwnershipID}

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}

	var gotOwnershipID string
	for _, tag := range putTagSet {
		if aws.ToString(tag.Key) == ownershipIDTagKey {
			gotOwnershipID = aws.ToString(tag.Value)
		}
	}
	if gotOwnershipID != testOwnershipID {
		t.Fatalf("expected the ownership-id tag to be backfilled to %q, got %q", testOwnershipID, gotOwnershipID)
	}
}

func TestEnsureBucketExists_ReclaimsAfterUIDChangeWhenOwnershipIDMatches(t *testing.T) {
	// The Velero-restore/cluster-migration case: a different UID owns the
	// tag (this Application's own previous incarnation), but the stable
	// ownership ID matches -- must reclaim automatically, no adopt-bucket
	// annotation required.
	var putTagSet []s3types.Tag
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
					{Key: aws.String(ownerNamespaceTagKey), Value: aws.String(testNamespace)},
					{Key: aws.String(ownershipIDTagKey), Value: aws.String(testOwnershipID)},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTagSet = params.Tagging.TagSet
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Annotations = map[string]string{naming.StorageOwnershipIDAnnotation: testOwnershipID}

	forgemetrics.StorageOwnershipReclaimedTotal.Reset()
	forgemetrics.StorageBucketAdoptedTotal.Reset()

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}

	var gotUID string
	for _, tag := range putTagSet {
		if aws.ToString(tag.Key) == ownerTagKey {
			gotUID = aws.ToString(tag.Value)
		}
	}
	if gotUID != string(testAppUID) {
		t.Fatalf("expected the owner tag to be rewritten to this Application's own UID %q, got %q", testAppUID, gotUID)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageOwnershipReclaimedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 1 {
		t.Fatalf("expected StorageOwnershipReclaimedTotal to be 1 after an automatic reclaim, got %v", got)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 0 {
		t.Fatalf("expected StorageBucketAdoptedTotal to stay 0 for an automatic reclaim, not a human-authorized adoption, got %v", got)
	}
}

func TestEnsureBucketExists_RefusesReclaimWhenOwnershipIDMismatched(t *testing.T) {
	// A different UID and a non-matching ownership ID: this is a genuinely
	// different Application, not a restore of this one -- must still
	// require the explicit adopt-bucket annotation, same as before this ID
	// existed at all.
	putTaggingCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
					{Key: aws.String(ownerNamespaceTagKey), Value: aws.String(testNamespace)},
					{Key: aws.String(ownershipIDTagKey), Value: aws.String("some-other-ownership-id")},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTaggingCalled = true
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Annotations = map[string]string{naming.StorageOwnershipIDAnnotation: testOwnershipID}

	forgemetrics.StorageOwnershipReclaimedTotal.Reset()

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned when the ownership ID doesn't match, got %v", err)
	}
	if putTaggingCalled {
		t.Fatalf("expected the tag to be left untouched when the ownership ID doesn't match")
	}
	if got := testutil.ToFloat64(forgemetrics.StorageOwnershipReclaimedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 0 {
		t.Fatalf("expected no reclaim to be recorded, got %v", got)
	}
}

func TestEnsureBucketExists_RefusesCrossNamespaceAdoptionWithoutGrant(t *testing.T) {
	putTaggingCalled := false
	s3Client := &mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
					{Key: aws.String(ownerNamespaceTagKey), Value: aws.String(testOwnerNamespace)},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTaggingCalled = true
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}

	// The adopting Application lives in a different namespace than the one
	// recorded as the bucket's previous owner, and that owner namespace
	// carries no naming.AllowBucketAdoptionFromAnnotation grant.
	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testAdopterNamespace, UID: testAppUID},
	}
	app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()

	m := &Manager{
		k8sClient: fakeClient,
		s3client:  s3Client,
		app:       app,
		storage:   &forgev1alpha1.StorageSpec{Bucket: testBucket, Region: testRegion},
		bucket:    testBucket,
		region:    testRegion,
	}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned for cross-namespace adoption without a grant, got %v", err)
	}
	if putTaggingCalled {
		t.Fatalf("expected the tag to be left untouched without a cross-namespace grant")
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 0 {
		t.Fatalf("expected no adoption to be recorded, got %v", got)
	}
}

func TestEnsureBucketExists_AllowsCrossNamespaceAdoptionWithGrant(t *testing.T) {
	var taggedNamespace string
	s3Client := &mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
					{Key: aws.String(ownerNamespaceTagKey), Value: aws.String(testOwnerNamespace)},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			for _, tag := range params.Tagging.TagSet {
				if aws.ToString(tag.Key) == ownerNamespaceTagKey {
					taggedNamespace = aws.ToString(tag.Value)
				}
			}
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}

	app := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testAdopterNamespace, UID: testAppUID},
	}
	app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	// The previous owner's namespace explicitly grants this adopting
	// namespace permission via naming.AllowBucketAdoptionFromAnnotation --
	// the "something a cluster admin grants" mechanism, since editing a
	// Namespace object needs separate RBAC from editing an Application.
	ownerNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testOwnerNamespace,
			Annotations: map[string]string{naming.AllowBucketAdoptionFromAnnotation: testAdopterNamespace},
		},
	}

	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app, ownerNamespace).WithStatusSubresource(app).Build()

	m := &Manager{
		k8sClient: fakeClient,
		s3client:  s3Client,
		app:       app,
		storage:   &forgev1alpha1.StorageSpec{Bucket: testBucket, Region: testRegion},
		bucket:    testBucket,
		region:    testRegion,
	}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if taggedNamespace != testAdopterNamespace {
		t.Fatalf("expected the owner-namespace tag to be rewritten to the adopting namespace %q, got %q", testAdopterNamespace, taggedNamespace)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 1 {
		t.Fatalf("expected StorageBucketAdoptedTotal to be 1 after a granted cross-namespace adoption, got %v", got)
	}
}

func TestEnsureBucketExists_RefusesAdoptionOfLegacyUntaggedNamespaceBucket(t *testing.T) {
	// A bucket adopted from a *different* Application, tagged before
	// namespace-scoped ownership shipped, carries no owner-namespace tag at
	// all. There's nothing to compare against, so this must fail closed
	// rather than silently treat it as same-namespace.
	putTaggingCalled := false
	s3Client := &mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTaggingCalled = true
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}

	m := newTestManager(s3Client, nil)
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned for a legacy untagged-namespace bucket, got %v", err)
	}
	if putTaggingCalled {
		t.Fatalf("expected the tag to be left untouched for a legacy bucket with no recorded owner namespace")
	}
}

func TestEnsureBucketExists_ClaimsEmptyTagSetWhenPreviouslyCreatedByUs(t *testing.T) {
	var taggedOwner string
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			for _, tag := range params.Tagging.TagSet {
				if aws.ToString(tag.Key) == ownerTagKey {
					taggedOwner = aws.ToString(tag.Value)
				}
			}
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Status.Storage = &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: true, CreatedAt: metav1.Now()}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	// A bucket that exists with no ownership tag at all is claimed when we
	// durably recorded creating it ourselves in an earlier reconcile -- this
	// is what recovers a bucket whose tag write failed transiently, since
	// that bucket looks identical to a genuinely foreign untagged one except
	// for this recorded intent.
	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if taggedOwner != string(testAppUID) {
		t.Fatalf("expected bucket to be claimed with owner UID, got %q", taggedOwner)
	}
	// This is recovery, not adoption -- previouslyCreatedByUs alone must
	// never be counted as taking over someone else's bucket.
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 0 {
		t.Fatalf("expected StorageBucketAdoptedTotal to stay 0 for a previouslyCreatedByUs recovery, got %v", got)
	}
}

func TestEnsureBucketExists_PreservesUnrelatedTagsWhenClaiming(t *testing.T) {
	// PutBucketTagging replaces a bucket's entire tag set rather than
	// merging into it -- so claiming ownership must round-trip whatever
	// unrelated tags (Terraform's default_tags, cost-allocation tags, ...)
	// the bucket already carried, not just write the ownership tag alone.
	unrelatedTag := s3types.Tag{Key: aws.String("cost-center"), Value: aws.String("platform")}
	var putTagSet []s3types.Tag
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{TagSet: []s3types.Tag{unrelatedTag}}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTagSet = params.Tagging.TagSet
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Status.Storage = &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: true, CreatedAt: metav1.Now()}

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}

	foundOwner, foundUnrelated := false, false
	for _, tag := range putTagSet {
		switch aws.ToString(tag.Key) {
		case ownerTagKey:
			foundOwner = aws.ToString(tag.Value) == string(testAppUID)
		case "cost-center":
			foundUnrelated = aws.ToString(tag.Value) == "platform"
		}
	}
	if !foundOwner {
		t.Fatalf("expected the ownership tag to be set, got %#v", putTagSet)
	}
	if !foundUnrelated {
		t.Fatalf("expected the pre-existing, unrelated tag to survive claiming ownership, got %#v", putTagSet)
	}
}

func TestEnsureBucketExists_PreservesUnrelatedTagsWhenAdopting(t *testing.T) {
	// Same preservation requirement on the adopt-bucket path, which
	// overwrites an existing (mismatched) ownership tag rather than adding
	// one to an empty set -- every *other* tag must still round-trip, and
	// the old owner's tag must not linger alongside the new one.
	unrelatedTag := s3types.Tag{Key: aws.String("cost-center"), Value: aws.String("platform")}
	var putTagSet []s3types.Tag
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					unrelatedTag,
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
					{Key: aws.String(ownerNamespaceTagKey), Value: aws.String(testNamespace)},
				},
			}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTagSet = params.Tagging.TagSet
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}

	ownerCount, foundOwner, foundUnrelated := 0, false, false
	for _, tag := range putTagSet {
		switch aws.ToString(tag.Key) {
		case ownerTagKey:
			ownerCount++
			foundOwner = aws.ToString(tag.Value) == string(testAppUID)
		case "cost-center":
			foundUnrelated = aws.ToString(tag.Value) == "platform"
		}
	}
	if ownerCount != 1 || !foundOwner {
		t.Fatalf("expected exactly one ownership tag set to this Application's UID, got %#v", putTagSet)
	}
	if !foundUnrelated {
		t.Fatalf("expected the pre-existing, unrelated tag to survive adoption, got %#v", putTagSet)
	}
}

func TestEnsureBucketExists_ClaimsEmptyTagSetWhenAdoptAnnotationSet(t *testing.T) {
	var taggedOwner string
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{}, nil
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			for _, tag := range params.Tagging.TagSet {
				if aws.ToString(tag.Key) == ownerTagKey {
					taggedOwner = aws.ToString(tag.Value)
				}
			}
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	// No Status.Storage seeded -- only the explicit human opt-in this time.
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if taggedOwner != string(testAppUID) {
		t.Fatalf("expected bucket to be claimed with owner UID, got %q", taggedOwner)
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAWSS3))); got != 1 {
		t.Fatalf("expected StorageBucketAdoptedTotal to be 1 after an actual adoption, got %v", got)
	}
}

func TestEnsureBucketExists_RejectsEmptyTagSetWithNeitherSignal(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{}, nil
		},
	}, nil)

	// No recorded creation, no adopt annotation: an untagged bucket must be
	// treated as genuinely foreign by default, not claimed.
	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestEnsureBucketExists_ClaimsOnNoSuchTagSetErrorWhenPreviouslyCreatedByUs(t *testing.T) {
	claimed := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode, Message: "The TagSet does not exist"}
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			claimed = true
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Status.Storage = &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: true, CreatedAt: metav1.Now()}

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !claimed {
		t.Fatalf("expected NoSuchTagSet to be treated as claimable, not rejected")
	}
}

func TestEnsureBucketExists_RejectsNoSuchTagSetWithNeitherSignal(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode, Message: "The TagSet does not exist"}
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

// --- recordBucketCreated / previouslyCreatedByUs ---

func TestRecordBucketCreated_PersistsCreatedFlagToStatus(t *testing.T) {
	m := newTestManager(nil, nil)

	if err := m.recordBucketCreated(context.Background()); err != nil {
		t.Fatalf("recordBucketCreated returned error: %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := m.k8sClient.Get(context.Background(), client.ObjectKey{Name: testAppName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("failed to get Application: %v", err)
	}
	if got.Status.Storage == nil || !got.Status.Storage.Created {
		t.Fatalf("expected Status.Storage.Created to be persisted true, got %#v", got.Status.Storage)
	}
	if got.Status.Storage.Bucket != testBucket {
		t.Fatalf("expected Status.Storage.Bucket to be %q, got %q", testBucket, got.Status.Storage.Bucket)
	}
	if got.Status.Storage.CreatedAt.IsZero() {
		t.Fatalf("expected Status.Storage.CreatedAt to be persisted non-zero")
	}
}

func TestPreviouslyCreatedByUs(t *testing.T) {
	now := metav1.Now()
	stale := metav1.NewTime(time.Now().Add(-2 * bucketCreationClaimWindow))

	tests := []struct {
		name    string
		storage *forgev1alpha1.StorageStatus
		want    bool
	}{
		{name: "nil status", storage: nil, want: false},
		{name: "matching bucket, created true, fresh", storage: &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: true, CreatedAt: now}, want: true},
		{name: "matching bucket, created false", storage: &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: false, CreatedAt: now}, want: false},
		{name: "different bucket, created true, fresh", storage: &forgev1alpha1.StorageStatus{Bucket: "some-other-bucket", Created: true, CreatedAt: now}, want: false},
		{
			name: "matching bucket, created true, but past the claim window",
			// The exact scenario bucketCreationClaimWindow exists to close:
			// Created/Bucket alone would still say "ours" here even though
			// this record is old enough that the name could plausibly have
			// been released and reused by something else entirely since.
			storage: &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: true, CreatedAt: stale},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestManager(nil, nil)
			m.app.Status.Storage = tt.storage
			if got := m.previouslyCreatedByUs(); got != tt.want {
				t.Errorf("previouslyCreatedByUs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureBucketExists_ReturnsNotOwnedWhenGetTaggingErrors(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			// A genuine, non-NoSuchTagSet failure (permission denied, network
			// error, ...) must NOT be treated as claimable -- only the
			// specific "no tags at all" case is safe to claim.
			return nil, errors.New("access denied")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestEnsureBucketExists_CreatesBucketOnTypedNotFound(t *testing.T) {
	createCalled := false
	var taggedBucket, taggedOwner string
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, &s3types.NotFound{}
		},
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			createCalled = true
			return &s3sdk.CreateBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode}
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			taggedBucket = aws.ToString(params.Bucket)
			for _, tag := range params.Tagging.TagSet {
				if aws.ToString(tag.Key) == ownerTagKey {
					taggedOwner = aws.ToString(tag.Value)
				}
			}
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateBucket to be called when bucket is not found")
	}
	if taggedBucket != testBucket || taggedOwner != string(testAppUID) {
		t.Fatalf("expected newly created bucket to be tagged with owner UID, got bucket=%q owner=%q", taggedBucket, taggedOwner)
	}
}

func TestEnsureBucketExists_CreatesBucketOn404ResponseError(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(404, "not found")
		},
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			createCalled = true
			return &s3sdk.CreateBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return nil, &smithy.GenericAPIError{Code: noSuchTagSetErrorCode}
		},
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.ensureBucketExists(context.Background()); err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateBucket to be called on 404 response error")
	}
}

func TestTagAsOwned_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			return nil, errors.New("tagging denied")
		},
	}, nil)

	if err := m.tagAsOwned(context.Background(), nil); err == nil {
		t.Fatalf("expected error from tagAsOwned, got nil")
	}
}

func TestTagAsOwned_IncludesOwnershipIDTagWhenSet(t *testing.T) {
	var putTagSet []s3types.Tag
	m := newTestManager(&mockS3Client{
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTagSet = params.Tagging.TagSet
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)
	m.app.Annotations = map[string]string{naming.StorageOwnershipIDAnnotation: testOwnershipID}

	if err := m.tagAsOwned(context.Background(), nil); err != nil {
		t.Fatalf("tagAsOwned returned error: %v", err)
	}

	var gotOwnershipID string
	for _, tag := range putTagSet {
		if aws.ToString(tag.Key) == ownershipIDTagKey {
			gotOwnershipID = aws.ToString(tag.Value)
		}
	}
	if gotOwnershipID != testOwnershipID {
		t.Fatalf("expected ownership-id tag %q, got %q", testOwnershipID, gotOwnershipID)
	}
}

func TestTagAsOwned_OmitsOwnershipIDTagWhenUnset(t *testing.T) {
	var putTagSet []s3types.Tag
	m := newTestManager(&mockS3Client{
		putBucketTaggingFunc: func(ctx context.Context, params *s3sdk.PutBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketTaggingOutput, error) {
			putTagSet = params.Tagging.TagSet
			return &s3sdk.PutBucketTaggingOutput{}, nil
		},
	}, nil)

	if err := m.tagAsOwned(context.Background(), nil); err != nil {
		t.Fatalf("tagAsOwned returned error: %v", err)
	}

	for _, tag := range putTagSet {
		if aws.ToString(tag.Key) == ownershipIDTagKey {
			t.Fatalf("expected no ownership-id tag when the Application carries none, got %q", aws.ToString(tag.Value))
		}
	}
}

func TestEnsureBucketExists_ReturnsErrorOn403(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(403, "access denied")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error on 403 response, got nil")
	}
}

func TestEnsureBucketExists_ReturnsErrorOn301(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(301, "wrong region")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error on 301 response, got nil")
	}
}

func TestEnsureBucketExists_ReturnsErrorOnUnexpectedStatus(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(500, "internal error")
		},
	}, nil)

	err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error on unexpected status, got nil")
	}
}

// --- CreateBucket ---

func TestCreateBucket_OmitsLocationConstraintForUSEast1(t *testing.T) {
	m := newTestManager(&mockS3Client{
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			if params.CreateBucketConfiguration != nil {
				t.Errorf("expected no CreateBucketConfiguration for us-east-1, got %#v", params.CreateBucketConfiguration)
			}
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)
	m.region = "us-east-1"

	if err := m.CreateBucket(context.Background()); err != nil {
		t.Fatalf("CreateBucket returned error: %v", err)
	}
}

func TestCreateBucket_SetsLocationConstraintForOtherRegions(t *testing.T) {
	m := newTestManager(&mockS3Client{
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			if params.CreateBucketConfiguration == nil {
				t.Fatalf("expected CreateBucketConfiguration to be set for non-default region")
			}
			if string(params.CreateBucketConfiguration.LocationConstraint) != testEUWestRegion {
				t.Errorf("expected LocationConstraint eu-west-1, got %q", params.CreateBucketConfiguration.LocationConstraint)
			}
			return &s3sdk.CreateBucketOutput{}, nil
		},
	}, nil)
	m.region = testEUWestRegion

	if err := m.CreateBucket(context.Background()); err != nil {
		t.Fatalf("CreateBucket returned error: %v", err)
	}
}

func TestCreateBucket_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		createBucketFunc: func(ctx context.Context, params *s3sdk.CreateBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.CreateBucketOutput, error) {
			return nil, errors.New("create failed")
		},
	}, nil)

	if err := m.CreateBucket(context.Background()); err == nil {
		t.Fatalf("expected error from CreateBucket, got nil")
	}
}

// --- ensureVersioning ---

func TestEnsureVersioning_Success(t *testing.T) {
	called := false
	m := newTestManager(&mockS3Client{
		putBucketVersioningFunc: func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
			called = true
			if params.VersioningConfiguration.Status != s3types.BucketVersioningStatusEnabled {
				t.Errorf("expected versioning status Enabled, got %q", params.VersioningConfiguration.Status)
			}
			return &s3sdk.PutBucketVersioningOutput{}, nil
		},
	}, nil)

	if err := m.ensureVersioning(context.Background()); err != nil {
		t.Fatalf("ensureVersioning returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected PutBucketVersioning to be called")
	}
}

func TestEnsureVersioning_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		putBucketVersioningFunc: func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
			return nil, errors.New("versioning failed")
		},
	}, nil)

	if err := m.ensureVersioning(context.Background()); err == nil {
		t.Fatalf("expected error from ensureVersioning, got nil")
	}
}

// --- ensureLifecyclePolicy ---

func TestEnsureLifecyclePolicy_Success(t *testing.T) {
	called := false
	m := newTestManager(&mockS3Client{
		putBucketLifecycleConfigFunc: func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
			called = true
			return &s3sdk.PutBucketLifecycleConfigurationOutput{}, nil
		},
	}, nil)

	if err := m.ensureLifecyclePolicy(context.Background()); err != nil {
		t.Fatalf("ensureLifecyclePolicy returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected PutBucketLifecycleConfiguration to be called")
	}
}

func TestEnsureLifecyclePolicy_PropagatesError(t *testing.T) {
	m := newTestManager(&mockS3Client{
		putBucketLifecycleConfigFunc: func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
			return nil, errors.New("lifecycle failed")
		},
	}, nil)

	if err := m.ensureLifecyclePolicy(context.Background()); err == nil {
		t.Fatalf("expected error from ensureLifecyclePolicy, got nil")
	}
}

// --- ReconcileAppIRSA ---

func TestReconcileAppIRSA_CreatesRoleWhenNotFound(t *testing.T) {
	createCalled := false
	putPolicyCalled := false
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return nil, &iamtypes.NoSuchEntityException{}
		},
		createRoleFunc: func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
			createCalled = true
			return &iam.CreateRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			putPolicyCalled = true
			return &iam.PutRolePolicyOutput{}, nil
		},
	})

	roleArn, err := m.ReconcileAppIRSA(context.Background())
	if err != nil {
		t.Fatalf("ReconcileAppIRSA returned error: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected CreateRole to be called when role does not exist")
	}
	if !putPolicyCalled {
		t.Fatalf("expected PutRolePolicy to be called")
	}
	if roleArn != testIRSARoleARN {
		t.Errorf("expected role arn to be returned, got %q", roleArn)
	}
}

func TestReconcileAppIRSA_SetsPermissionsBoundaryOnCreate(t *testing.T) {
	// Terraform's IAMRoleCreationForApps statement rejects any CreateRole
	// call for an app-irsa-* role that omits this boundary or names a
	// different one -- without it, ReconcileAppIRSA would fail at the AWS
	// layer regardless of what this test's fake IAM client returns.
	var gotBoundary string
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return nil, &iamtypes.NoSuchEntityException{}
		},
		createRoleFunc: func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
			gotBoundary = aws.ToString(params.PermissionsBoundary)
			return &iam.CreateRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			return &iam.PutRolePolicyOutput{}, nil
		},
	})

	if _, err := m.ReconcileAppIRSA(context.Background()); err != nil {
		t.Fatalf("ReconcileAppIRSA returned error: %v", err)
	}
	if gotBoundary != testPermissionsBoundary {
		t.Fatalf("expected CreateRoleInput.PermissionsBoundary %q, got %q", testPermissionsBoundary, gotBoundary)
	}
}

func TestReconcileAppIRSA_UpdatesTrustPolicyWhenRoleExists(t *testing.T) {
	updateCalled := false
	createCalled := false
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		createRoleFunc: func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
			createCalled = true
			return &iam.CreateRoleOutput{}, nil
		},
		updateAssumeRolePolicyFunc: func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
			updateCalled = true
			return &iam.UpdateAssumeRolePolicyOutput{}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			return &iam.PutRolePolicyOutput{}, nil
		},
	})

	roleArn, err := m.ReconcileAppIRSA(context.Background())
	if err != nil {
		t.Fatalf("ReconcileAppIRSA returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected CreateRole not to be called when role already exists")
	}
	if !updateCalled {
		t.Fatalf("expected UpdateAssumeRolePolicy to be called when role already exists")
	}
	if roleArn != testIRSARoleARN {
		t.Errorf("expected role arn to be returned, got %q", roleArn)
	}
}

func TestReconcileAppIRSA_PropagatesCreateRoleError(t *testing.T) {
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return nil, &iamtypes.NoSuchEntityException{}
		},
		createRoleFunc: func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
			return nil, errors.New("create role failed")
		},
	})

	_, err := m.ReconcileAppIRSA(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileAppIRSA, got nil")
	}
}

func TestReconcileAppIRSA_PropagatesPutRolePolicyError(t *testing.T) {
	m := newTestManager(nil, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		updateAssumeRolePolicyFunc: func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
			return &iam.UpdateAssumeRolePolicyOutput{}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			return nil, errors.New("put policy failed")
		},
	})

	_, err := m.ReconcileAppIRSA(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileAppIRSA, got nil")
	}
}

// --- ReconcileBucket ---

func TestReconcileBucket_HappyPathReturnsRoleARN(t *testing.T) {
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return matchingTagging(), nil
		},
		putBucketVersioningFunc: func(ctx context.Context, params *s3sdk.PutBucketVersioningInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketVersioningOutput, error) {
			return &s3sdk.PutBucketVersioningOutput{}, nil
		},
		putBucketLifecycleConfigFunc: func(ctx context.Context, params *s3sdk.PutBucketLifecycleConfigurationInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutBucketLifecycleConfigurationOutput, error) {
			return &s3sdk.PutBucketLifecycleConfigurationOutput{}, nil
		},
	}, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			return &iam.GetRoleOutput{
				Role: &iamtypes.Role{Arn: aws.String(testIRSARoleARN)},
			}, nil
		},
		updateAssumeRolePolicyFunc: func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
			return &iam.UpdateAssumeRolePolicyOutput{}, nil
		},
		putRolePolicyFunc: func(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
			return &iam.PutRolePolicyOutput{}, nil
		},
	})

	result, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("ReconcileBucket returned error: %v", err)
	}
	if result.RoleARN != testIRSARoleARN {
		t.Errorf("expected role arn to be returned, got %q", result.RoleARN)
	}
}

func TestReconcileBucket_ShortCircuitsOnBucketError(t *testing.T) {
	iamCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return nil, newHTTPStatusError(403, "access denied")
		},
	}, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			iamCalled = true
			return &iam.GetRoleOutput{}, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileBucket, got nil")
	}
	if iamCalled {
		t.Fatalf("expected IRSA reconciliation to be skipped after bucket error")
	}
}

func TestReconcileBucket_ShortCircuitsOnNotOwnedBucket(t *testing.T) {
	iamCalled := false
	m := newTestManager(&mockS3Client{
		headBucketFunc: func(ctx context.Context, params *s3sdk.HeadBucketInput, optFns ...func(*s3sdk.Options)) (*s3sdk.HeadBucketOutput, error) {
			return &s3sdk.HeadBucketOutput{}, nil
		},
		getBucketTaggingFunc: func(ctx context.Context, params *s3sdk.GetBucketTaggingInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetBucketTaggingOutput, error) {
			return &s3sdk.GetBucketTaggingOutput{
				TagSet: []s3types.Tag{
					{Key: aws.String(ownerTagKey), Value: aws.String(string(testOtherUID))},
				},
			}, nil
		},
	}, &mockIAMClient{
		getRoleFunc: func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
			iamCalled = true
			return &iam.GetRoleOutput{}, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned from ReconcileBucket, got %v", err)
	}
	if iamCalled {
		t.Fatalf("expected IRSA reconciliation to be skipped when bucket isn't owned by this Application")
	}
}

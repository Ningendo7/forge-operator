package akamaiobjstr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
	"github.com/Ningendo7/forge-operator/internal/controller/naming"
	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/linodego"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func notFoundErr() error {
	return &linodego.Error{Code: http.StatusNotFound}
}

// --- validateStorageSpec ---

func TestValidateStorageSpec(t *testing.T) {
	tests := []struct {
		name    string
		storage *forgev1alpha1.StorageSpec
		bucket  string
		region  string
		wantErr bool
	}{
		{name: "nil storage spec", storage: nil, bucket: "b", region: "r", wantErr: true},
		{name: "empty bucket", storage: &forgev1alpha1.StorageSpec{}, bucket: "", region: "r", wantErr: true},
		{name: "empty region", storage: &forgev1alpha1.StorageSpec{}, bucket: "b", region: "", wantErr: true},
		{name: "valid spec", storage: &forgev1alpha1.StorageSpec{}, bucket: "b", region: "r", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestManager(nil)
			m.storage = tt.storage
			m.bucket = tt.bucket
			m.region = tt.region

			err := m.validateStorageSpec()
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

// --- ensureBucketExists ---

func TestEnsureBucketExists_ReturnsExistingBucket(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		createObjectStorageBucketFunc: func(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error) {
			createCalled = true
			return &linodego.ObjectStorageBucket{}, nil
		},
	})

	bucket, err := m.ensureBucketExists(context.Background())
	if err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if bucket == nil {
		t.Fatalf("expected bucket to be returned")
	}
	if createCalled {
		t.Fatalf("expected CreateObjectStorageBucket not to be called when bucket exists")
	}
}

func TestEnsureBucketExists_CreatesBucketWhenNotFound(t *testing.T) {
	createCalled := false
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, notFoundErr()
		},
		createObjectStorageBucketFunc: func(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error) {
			createCalled = true
			if opts.Label != testBucket || opts.Region != testRegion {
				t.Errorf("unexpected create options: %#v", opts)
			}
			return &linodego.ObjectStorageBucket{Label: opts.Label}, nil
		},
	})

	bucket, err := m.ensureBucketExists(context.Background())
	if err != nil {
		t.Fatalf("ensureBucketExists returned error: %v", err)
	}
	if bucket == nil {
		t.Fatalf("expected bucket to be returned")
	}
	if !createCalled {
		t.Fatalf("expected CreateObjectStorageBucket to be called when bucket is not found")
	}
}

func TestEnsureBucketExists_PropagatesGetError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, errors.New("query failed")
		},
	})

	_, err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureBucketExists, got nil")
	}
}

func TestEnsureBucketExists_PropagatesCreateError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, notFoundErr()
		},
		createObjectStorageBucketFunc: func(ctx context.Context, opts linodego.ObjectStorageBucketCreateOptions) (*linodego.ObjectStorageBucket, error) {
			return nil, errors.New("create failed")
		},
	})

	_, err := m.ensureBucketExists(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureBucketExists, got nil")
	}
}

// --- ensureAccessKey ---

func TestEnsureAccessKey_ReusesExistingKeyWithRecoverableSecret(t *testing.T) {
	createCalled := false
	deleteCalled := false
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{
				{ID: 42, Label: testAccessKeyLabel, AccessKey: testExistingAccessKey},
			}, nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			createCalled = true
			return &linodego.ObjectStorageKey{}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			deleteCalled = true
			return nil
		},
	})

	// The operator's own previously-written output Secret already has a
	// recorded secret key from an earlier, fully-successful reconcile --
	// the common case for any reconcile after the first.
	storageSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testStorageSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretKeyDataKey: []byte("recovered-secret")},
	}
	if err := m.k8sClient.Create(context.Background(), storageSecret); err != nil {
		t.Fatalf("failed to seed storage Secret: %v", err)
	}

	result, err := m.ensureAccessKey(context.Background())
	if err != nil {
		t.Fatalf("ensureAccessKey returned error: %v", err)
	}
	if result.AccessKey != testExistingAccessKey {
		t.Errorf("expected existing access key to be reused, got %q", result.AccessKey)
	}
	if result.SecretKey != "recovered-secret" {
		t.Errorf("expected the secret recovered from the output Secret, got %q", result.SecretKey)
	}
	if createCalled {
		t.Fatalf("expected CreateObjectStorageKey not to be called when the secret was recoverable")
	}
	if deleteCalled {
		t.Fatalf("expected DeleteObjectStorageKey not to be called when the secret was recoverable")
	}
}

func TestEnsureAccessKey_ReplacesKeyWhenSecretUnrecoverable(t *testing.T) {
	deletedKeyID := -1
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{
				{ID: 42, Label: testAccessKeyLabel, AccessKey: testExistingAccessKey},
			}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			deletedKeyID = keyID
			return nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			return &linodego.ObjectStorageKey{AccessKey: testNewAccessKey, SecretKey: testNewSecretKey}, nil
		},
	})
	// Deliberately no output Secret seeded -- an earlier reconcile created
	// this key but must have failed before ever reaching the step that
	// writes it, so there's no way to recover the real secret. Nothing can
	// be depending on these credentials since they were never exposed
	// anywhere, so replacing the key outright is the correct, safe move.

	result, err := m.ensureAccessKey(context.Background())
	if err != nil {
		t.Fatalf("ensureAccessKey returned error: %v", err)
	}
	if deletedKeyID != 42 {
		t.Fatalf("expected the unusable key (ID 42) to be deleted, got deletedKeyID=%d", deletedKeyID)
	}
	if result.AccessKey != testNewAccessKey || result.SecretKey != testNewSecretKey {
		t.Errorf("expected the newly created key's credentials to be returned, got %#v", result)
	}
}

func TestEnsureAccessKey_PropagatesDeleteErrorWhenSecretUnrecoverable(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{
				{ID: 42, Label: testAccessKeyLabel, AccessKey: testExistingAccessKey},
			}, nil
		},
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			return errors.New("delete failed")
		},
	})

	if _, err := m.ensureAccessKey(context.Background()); err == nil {
		t.Fatalf("expected error from ensureAccessKey when deleting the unusable key fails, got nil")
	}
}

func TestEnsureAccessKey_CreatesNewKeyWhenNoneExists(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			if opts.Label != testAccessKeyLabel {
				t.Errorf("expected key label demo-app-key, got %q", opts.Label)
			}
			// The whole reason BucketAccess is used instead of an unscoped
			// key: without a correct BucketName, the generated key would
			// grant access beyond this Application's own bucket.
			if opts.BucketAccess == nil || len(*opts.BucketAccess) != 1 {
				t.Fatalf("expected exactly one BucketAccess entry, got %#v", opts.BucketAccess)
			}
			access := (*opts.BucketAccess)[0]
			if access.BucketName != testBucket {
				t.Errorf("expected key scoped to bucket %q, got %q", testBucket, access.BucketName)
			}
			if access.Permissions != "read_write" {
				t.Errorf("expected read_write permissions, got %q", access.Permissions)
			}
			return &linodego.ObjectStorageKey{AccessKey: testNewAccessKey, SecretKey: testNewSecretKey}, nil
		},
	})

	result, err := m.ensureAccessKey(context.Background())
	if err != nil {
		t.Fatalf("ensureAccessKey returned error: %v", err)
	}
	if result.AccessKey != testNewAccessKey || result.SecretKey != testNewSecretKey {
		t.Errorf("expected new key credentials to be returned, got %#v", result)
	}
}

func TestEnsureAccessKey_PropagatesListError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return nil, errors.New("list failed")
		},
	})

	_, err := m.ensureAccessKey(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureAccessKey, got nil")
	}
}

func TestEnsureAccessKey_PropagatesCreateError(t *testing.T) {
	m := newTestManager(&mockAkamaiClient{
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{}, nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			return nil, errors.New("create failed")
		},
	})

	_, err := m.ensureAccessKey(context.Background())
	if err == nil {
		t.Fatalf("expected error from ensureAccessKey, got nil")
	}
}

// --- resolveEndpoint ---

func TestResolveEndpoint_UsesConfiguredEndpoint(t *testing.T) {
	m := newTestManager(nil)
	m.storage.Endpoint = "custom.endpoint.example.com"

	bucket := &linodego.ObjectStorageBucket{Hostname: "bucket.us-east-1.linodeobjects.com"}
	if got := m.resolveEndpoint(bucket); got != "custom.endpoint.example.com" {
		t.Fatalf("expected configured endpoint, got %q", got)
	}
}

func TestResolveEndpoint_PrefersBucketHostname(t *testing.T) {
	m := newTestManager(nil)
	m.storage.Endpoint = ""
	m.region = testRegion

	bucket := &linodego.ObjectStorageBucket{Hostname: "bucket-label.us-east-1.linodeobjects.com"}
	if got := m.resolveEndpoint(bucket); got != "bucket-label.us-east-1.linodeobjects.com" {
		t.Fatalf("expected bucket hostname, got %q", got)
	}
}

func TestResolveEndpoint_DefaultsToRegionBasedEndpoint(t *testing.T) {
	m := newTestManager(nil)
	m.storage.Endpoint = ""
	m.region = testRegion

	// No bucket at all, and a bucket with no Hostname, both fall back the
	// same way.
	if got := m.resolveEndpoint(nil); got != testDefaultEndpoint {
		t.Fatalf("expected default region-based endpoint, got %q", got)
	}
	if got := m.resolveEndpoint(&linodego.ObjectStorageBucket{}); got != testDefaultEndpoint {
		t.Fatalf("expected default region-based endpoint, got %q", got)
	}
}

// --- ReconcileBucket ---

func TestReconcileBucket_HappyPath(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testAppUID)))}, nil
		},
	})

	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{ID: 42, Label: testAccessKeyLabel, AccessKey: testExistingAccessKey}}, nil
		},
	})
	// The bucket and marker already existing and matching implies an
	// earlier reconcile already succeeded end-to-end, which would also have
	// already written the output Secret -- seeded here so ensureAccessKey's
	// reuse path can recover a real secret, same as a real established
	// Application would see.
	storageSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testStorageSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretKeyDataKey: []byte("recovered-secret")},
	}
	if err := m.k8sClient.Create(context.Background(), storageSecret); err != nil {
		t.Fatalf("failed to seed storage Secret: %v", err)
	}

	result, err := m.ReconcileBucket(context.Background())
	if err != nil {
		t.Fatalf("ReconcileBucket returned error: %v", err)
	}
	if result.AccessKey != testExistingAccessKey {
		t.Errorf("expected access key to be returned, got %q", result.AccessKey)
	}
	if result.Endpoint != testDefaultEndpoint {
		t.Errorf("expected default endpoint, got %q", result.Endpoint)
	}
}

// --- claimOrVerifyOwnership / claimOwnership ---

func TestClaimOrVerifyOwnership_ClaimsMissingMarkerWhenPreviouslyCreatedByUs(t *testing.T) {
	claimed := false
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return nil, &s3types.NoSuchKey{}
		},
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			claimed = true
			body, _ := io.ReadAll(params.Body)
			if string(body) != string(testAppUID) {
				t.Errorf("expected marker body to be the app UID, got %q", body)
			}
			return &s3sdk.PutObjectOutput{}, nil
		},
	})

	m := newTestManager(nil)
	m.app.Status.Storage = &forgev1alpha1.StorageStatus{Bucket: testBucket, Created: true, CreatedAt: metav1.Now()}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	if err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk"); err != nil {
		t.Fatalf("claimOrVerifyOwnership returned error: %v", err)
	}
	if !claimed {
		t.Fatalf("expected a missing marker to be claimed when previously created by us")
	}
	// This is recovery, not adoption -- previouslyCreatedByUs alone must
	// never be counted as taking over someone else's bucket.
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage))); got != 0 {
		t.Fatalf("expected StorageBucketAdoptedTotal to stay 0 for a previouslyCreatedByUs recovery, got %v", got)
	}
}

func TestClaimOrVerifyOwnership_ClaimsMissingMarkerWhenAdoptAnnotationSet(t *testing.T) {
	claimed := false
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return nil, &s3types.NoSuchKey{}
		},
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			claimed = true
			return &s3sdk.PutObjectOutput{}, nil
		},
	})

	m := newTestManager(nil)
	// No Status.Storage seeded -- only the explicit human opt-in this time.
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	if err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk"); err != nil {
		t.Fatalf("claimOrVerifyOwnership returned error: %v", err)
	}
	if !claimed {
		t.Fatalf("expected a missing marker to be claimed when adopt annotation is set")
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage))); got != 1 {
		t.Fatalf("expected StorageBucketAdoptedTotal to be 1 after an actual adoption, got %v", got)
	}
}

func TestClaimOrVerifyOwnership_RejectsMissingMarkerWithNeitherSignal(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return nil, &s3types.NoSuchKey{}
		},
	})

	m := newTestManager(nil)

	// No recorded creation, no adopt annotation: a marker-less bucket must
	// be treated as genuinely foreign by default, not claimed.
	err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk")
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

// --- recordBucketCreated / previouslyCreatedByUs ---

func TestRecordBucketCreated_PersistsCreatedFlagToStatus(t *testing.T) {
	m := newTestManager(nil)

	if err := m.recordBucketCreated(context.Background()); err != nil {
		t.Fatalf("recordBucketCreated returned error: %v", err)
	}

	got := &forgev1alpha1.Application{}
	if err := m.k8sClient.Get(context.Background(), client.ObjectKey{Name: "demo-app", Namespace: testNamespace}, got); err != nil {
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
			m := newTestManager(nil)
			m.app.Status.Storage = tt.storage
			if got := m.previouslyCreatedByUs(); got != tt.want {
				t.Errorf("previouslyCreatedByUs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimOrVerifyOwnership_ProceedsWhenMarkerMatches(t *testing.T) {
	putCalled := false
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testAppUID)))}, nil
		},
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			putCalled = true
			return &s3sdk.PutObjectOutput{}, nil
		},
	})

	m := newTestManager(nil)

	if err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk"); err != nil {
		t.Fatalf("claimOrVerifyOwnership returned error: %v", err)
	}
	if putCalled {
		t.Fatalf("expected no write when the marker already matches")
	}
}

func TestClaimOrVerifyOwnership_ReturnsNotOwnedWhenMarkerMismatched(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testOtherUID)))}, nil
		},
	})

	m := newTestManager(nil)

	err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk")
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestClaimOrVerifyOwnership_AdoptsMismatchedMarkerWhenAnnotationSet(t *testing.T) {
	claimed := false
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testOtherUID)))}, nil
		},
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			claimed = true
			body, _ := io.ReadAll(params.Body)
			if string(body) != string(testAppUID) {
				t.Errorf("expected the marker to be rewritten to this Application's own UID, got %q", body)
			}
			return &s3sdk.PutObjectOutput{}, nil
		},
	})

	m := newTestManager(nil)
	m.app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	// A marker naming a *different* Application must still be adoptable
	// via the explicit annotation -- this is the deliberate human-in-the-
	// loop override, distinct from (and not gated by) previouslyCreatedByUs,
	// which by definition can never be true for a bucket someone else made.
	if err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk"); err != nil {
		t.Fatalf("claimOrVerifyOwnership returned error: %v", err)
	}
	if !claimed {
		t.Fatalf("expected a mismatched marker to be overwritten when the adopt annotation is set")
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage))); got != 1 {
		t.Fatalf("expected StorageBucketAdoptedTotal to be 1 after an actual adoption, got %v", got)
	}
}

func TestClaimOrVerifyOwnership_RefusesToAdoptWhenPreviousOwnerStillExists(t *testing.T) {
	putCalled := false
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testOtherUID)))}, nil
		},
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			putCalled = true
			return &s3sdk.PutObjectOutput{}, nil
		},
	})

	app := newTestApp()
	app.Annotations = map[string]string{naming.AdoptBucketAnnotation: naming.AdoptBucketAnnotationValue}
	// The Application actually named by the marker's UID is still very
	// much alive -- adopt-bucket must not be able to take its bucket away
	// from it just because the annotation is set.
	stillAliveOwner := &forgev1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "still-alive-owner", Namespace: testNamespace, UID: testOtherUID},
	}

	scheme := runtime.NewScheme()
	_ = forgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app, stillAliveOwner).WithStatusSubresource(app).Build()

	m := &Manager{
		k8sClient: fakeClient,
		app:       app,
		storage:   &forgev1alpha1.StorageSpec{Bucket: testBucket, Region: testRegion},
		bucket:    testBucket,
		region:    testRegion,
	}

	forgemetrics.StorageBucketAdoptedTotal.Reset()

	err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk")
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned when the previous owner still exists, got %v", err)
	}
	if putCalled {
		t.Fatalf("expected the marker to be left untouched when the previous owner still exists")
	}
	if got := testutil.ToFloat64(forgemetrics.StorageBucketAdoptedTotal.WithLabelValues(string(forgev1alpha1.ProviderAkamaiObjectStorage))); got != 0 {
		t.Fatalf("expected no adoption to be recorded, got %v", got)
	}
}

func TestClaimOrVerifyOwnership_ReturnsNotOwnedOnGenericGetError(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			// A genuine failure (permission denied, network error, ...) must
			// NOT be treated as claimable -- only a typed NoSuchKey is.
			return nil, errors.New("access denied")
		},
	})

	m := newTestManager(nil)

	err := m.claimOrVerifyOwnership(context.Background(), "demo-bucket.us-iad-10.linodeobjects.com", "ak", "sk")
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned, got %v", err)
	}
}

func TestClaimOwnership_PropagatesPutError(t *testing.T) {
	m := newTestManager(nil)

	err := m.claimOwnership(context.Background(), &mockS3ObjectClient{
		putObjectFunc: func(ctx context.Context, params *s3sdk.PutObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.PutObjectOutput, error) {
			return nil, errors.New("write denied")
		},
	})
	if err == nil {
		t.Fatalf("expected error from claimOwnership, got nil")
	}
}

func TestReconcileBucket_ReturnsErrorOnInvalidSpec(t *testing.T) {
	m := newTestManager(nil)
	m.storage = nil

	_, err := m.ReconcileBucket(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileBucket with nil storage spec, got nil")
	}
}

func TestReconcileBucket_ShortCircuitsOnBucketError(t *testing.T) {
	keyListCalled := false
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return nil, errors.New("bucket query failed")
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			keyListCalled = true
			return nil, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if err == nil {
		t.Fatalf("expected error from ReconcileBucket, got nil")
	}
	if keyListCalled {
		t.Fatalf("expected access key reconciliation to be skipped after bucket error")
	}
}

func TestReconcileBucket_ShortCircuitsOnNotOwnedBucket(t *testing.T) {
	withS3ObjectClient(t, &mockS3ObjectClient{
		getObjectFunc: func(ctx context.Context, params *s3sdk.GetObjectInput, optFns ...func(*s3sdk.Options)) (*s3sdk.GetObjectOutput, error) {
			return &s3sdk.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(testOtherUID)))}, nil
		},
	})

	// Stateful, mirroring the real API: findAccessKeyIDByLabel must see the
	// newly-created key (43), not the stale one ensureAccessKey already
	// deleted (42), or this test can't tell "leaked the pre-existing key
	// again" apart from "correctly cleaned up the new one".
	currentKeyID := 42
	var deletedKeyIDs []int
	m := newTestManager(&mockAkamaiClient{
		getObjectStorageBucketFunc: func(ctx context.Context, clusterID, bucket string) (*linodego.ObjectStorageBucket, error) {
			return &linodego.ObjectStorageBucket{Label: bucket}, nil
		},
		listObjectStorageKeysFunc: func(ctx context.Context, opts *linodego.ListOptions) ([]linodego.ObjectStorageKey, error) {
			return []linodego.ObjectStorageKey{{ID: currentKeyID, Label: testAccessKeyLabel, AccessKey: testExistingAccessKey}}, nil
		},
		// No output Secret is seeded (irrelevant to what this test actually
		// checks), so ensureAccessKey takes its delete-and-recreate path --
		// this no-op is just there to satisfy that, not to assert anything
		// about it.
		deleteObjectStorageKeyFunc: func(ctx context.Context, keyID int) error {
			deletedKeyIDs = append(deletedKeyIDs, keyID)
			return nil
		},
		createObjectStorageKeyFunc: func(ctx context.Context, opts linodego.ObjectStorageKeyCreateOptions) (*linodego.ObjectStorageKey, error) {
			currentKeyID = 43
			return &linodego.ObjectStorageKey{ID: currentKeyID, AccessKey: testExistingAccessKey}, nil
		},
	})

	_, err := m.ReconcileBucket(context.Background())
	if !errors.Is(err, ErrBucketNotOwned) {
		t.Fatalf("expected ErrBucketNotOwned from ReconcileBucket, got %v", err)
	}
	// ensureAccessKey necessarily runs before ownership can even be
	// checked -- confirmed live, via this same package's integration
	// tests, that the access key it creates was silently leaked whenever
	// ownership then turned out to fail. Expect both deletions: 42 (the
	// stale key ensureAccessKey replaced on its way in) and 43 (the fresh
	// key that replaced it, now cleaned up since ownership failed).
	if len(deletedKeyIDs) != 2 || deletedKeyIDs[0] != 42 || deletedKeyIDs[1] != 43 {
		t.Fatalf("expected DeleteObjectStorageKey(42) then DeleteObjectStorageKey(43), got %v", deletedKeyIDs)
	}
}
